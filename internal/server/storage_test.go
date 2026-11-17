package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// storageBox serves one anonymised USB disk and records writes.
type storageBox struct {
	mu     sync.Mutex
	writes []string
}

func (b *storageBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "storage/disk/":
		body = `[{"id":1001,"type":"usb","state":"enabled","model":"Test Stick","serial":"SN0001","table_type":"gpt","total_bytes":8000000000,"temp":35,
		 "read_error_requests":0,"write_error_requests":3,
		 "partitions":[{"id":1002,"disk_id":1001,"state":"mounted","fstype":"ext4","label":"Cle","total_bytes":7000000000,"used_bytes":1750000000,"free_bytes":5250000000,"fsck_result":"fs_clean"}]}]`
	case "storage/config/":
		body = `{"external_pm_enabled":true,"external_pm_idle_before_spindown":10}`
	case "storage/raid/":
		return errors.New("freebox: Erreur interne (internal_error)")
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *storageBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	j, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(j))
	b.mu.Unlock()
	return nil
}

func (b *storageBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true}, nil
}

func storageSetup(t *testing.T) (*storageBox, *http.Client, string, func(string, url.Values) *http.Response) {
	t.Helper()
	box := &storageBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	return box, c, srv.URL, func(p string, f url.Values) *http.Response { return post(t, c, srv, p, f) }
}

func TestStoragePage(t *testing.T) {
	box := &storageBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/storage")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d (a RAID error must not break the page)\n%s", resp.StatusCode, body)
	}
	for _, want := range []string{"Test Stick", "SN0001", "35 °C", "1.6 Go (25 %)", "4.9 Go", "Cle", "0 / 3", "Éjecter", "Formater", "Démonter"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "<h2>RAID</h2>") {
		t.Error("RAID section shown without arrays")
	}
}

func TestStorageWrites(t *testing.T) {
	box, _, _, send := storageSetup(t)
	last := func() string { return box.writes[len(box.writes)-1] }

	send("/storage/disks/1001/state", url.Values{"state": {"disabled"}, "confirm": {"1"}})
	if got := last(); got != `PUT storage/disk/1001 {"state":"disabled"}` {
		t.Errorf("eject: %s", got)
	}
	send("/storage/disks/1001/state", url.Values{"state": {"enabled"}})
	if got := last(); got != `PUT storage/disk/1001 {"state":"enabled"}` {
		t.Errorf("enable: %s", got)
	}
	send("/storage/disks/1001/format", url.Values{"table_type": {"gpt"}, "fs_type": {"ext4"}, "label": {"data"}, "confirm": {"1"}})
	if got := last(); got != `PUT storage/disk/1001/format/ {"fs_type":"ext4","label":"data","table_type":"gpt"}` {
		t.Errorf("format: %s", got)
	}
	send("/storage/partitions/1002/state", url.Values{"state": {"umounted"}, "confirm": {"1"}})
	if got := last(); got != `PUT storage/partition/1002 {"state":"umounted"}` {
		t.Errorf("umount: %s", got)
	}
	send("/storage/partitions/1002/check", url.Values{"checkmode": {"rw"}, "confirm": {"1"}})
	if got := last(); got != `PUT storage/partition/1002/check/ {"checkmode":"rw"}` {
		t.Errorf("check: %s", got)
	}
	send("/storage/config", url.Values{"enabled": {"1"}, "idle": {"15"}})
	if got := last(); got != `PUT storage/config/ {"external_pm_enabled":true,"external_pm_idle_before_spindown":15}` {
		t.Errorf("config: %s", got)
	}
}

func TestStorageCheck(t *testing.T) {
	box, _, _, send := storageSetup(t)
	send("/storage/partitions/1002/check", url.Values{"checkmode": {"ro"}})
	send("/storage/partitions/1002/check", url.Values{"checkmode": {"rw"}, "confirm": {"1"}})
	want := []string{`PUT storage/partition/1002/check/ {"checkmode":"ro"}`, `PUT storage/partition/1002/check/ {"checkmode":"rw"}`}
	if strings.Join(box.writes, "\n") != strings.Join(want, "\n") {
		t.Errorf("writes %v, want %v", box.writes, want)
	}
}

func TestStorageDangerousNeedConfirm(t *testing.T) {
	box, _, _, send := storageSetup(t)
	send("/storage/disks/1001/state", url.Values{"state": {"disabled"}})
	send("/storage/disks/1001/format", url.Values{"table_type": {"gpt"}, "fs_type": {"ext4"}})
	send("/storage/partitions/1002/state", url.Values{"state": {"umounted"}})
	send("/storage/partitions/1002/check", url.Values{"checkmode": {"rw"}})
	// Bad values and ids never reach the box either.
	send("/storage/disks/1001/format", url.Values{"table_type": {"x"}, "fs_type": {"ext4"}, "confirm": {"1"}})
	send("/storage/disks/abc/state", url.Values{"state": {"enabled"}})
	send("/storage/config", url.Values{"idle": {"0"}})
	if len(box.writes) != 0 {
		t.Fatalf("unsafe requests reached the box: %v", box.writes)
	}
}
