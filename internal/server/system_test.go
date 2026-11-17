package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type systemBox struct {
	mu     sync.Mutex
	writes []string
}

func (b *systemBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "system/":
		body = `{"mac":"00:11:22:33:44:55","serial":"0000000000000000","board_name":"fbxgw8r","firmware_version":"4.13.5","uptime_val":90061,"disk_status":"active","usb3_enable":true,
			"model_info":{"pretty_name":"Freebox v8 (r1)"},"sensors":[{"name":"Température 1","value":60}],"fans":[{"name":"Ventilateur 1","value":1695}]}`
	case "update/":
		body = `{"state":"up_to_date"}`
	case "lang/":
		body = `{"lang":"fra","avalaible":["fra","eng"]}`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *systemBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	j, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(j))
	b.mu.Unlock()
	return nil
}

func (b *systemBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true}, nil
}

func TestSystemPage(t *testing.T) {
	box := &systemBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))

	resp, body := get(t, c, srv, "/system")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"Freebox v8 (r1)", "4.13.5", "1 j 1 h", "À jour", "Français", "English", "Numéro de série", "1695 tr/min"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// The serial sits inside a closed <details>, not in plain sight.
	i := strings.Index(body, "0000000000000000")
	if i < 0 || strings.LastIndex(body[:i], "<details") < strings.LastIndex(body[:i], "</details>") {
		t.Error("serial outside details")
	}
}

func TestSystemWrites(t *testing.T) {
	box := &systemBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))

	post(t, c, srv, "/system/lang", map[string][]string{"lang": {"eng"}})
	post(t, c, srv, "/system/reboot", nil)
	post(t, c, srv, "/system/shutdown", nil)
	if len(box.writes) != 1 || box.writes[0] != `POST lang/ {"lang":"eng"}` {
		t.Fatalf("writes without confirm: %v", box.writes)
	}
	post(t, c, srv, "/system/reboot", map[string][]string{"confirm": {"1"}})
	post(t, c, srv, "/system/shutdown", map[string][]string{"confirm": {"1"}})
	want := []string{"POST system/reboot/ null", "POST system/shutdown/ null"}
	if len(box.writes) != 3 || box.writes[1] != want[0] || box.writes[2] != want[1] {
		t.Fatalf("writes: %v", box.writes)
	}
}
