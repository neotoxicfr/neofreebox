package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// filesBox serves anonymised fs, share_link and task answers and records writes.
type filesBox struct {
	mu     sync.Mutex
	writes []string
}

func (b *filesBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch {
	case strings.HasPrefix(path, "fs/ls/"):
		body = `{"entries":[
		 {"path":"` + enc("/Disque/.") + `","name":".","type":"dir","hidden":true},
		 {"path":"` + enc("/Disque/zeta.txt") + `","name":"zeta.txt","type":"file","size":2048,"modification":1700000000},
		 {"path":"` + enc("/Disque/Photos") + `","name":"Photos","type":"dir","size":4096,"modification":1700000000},
		 {"path":"` + enc("/Disque/alpha.zip") + `","name":"alpha.zip","type":"file","size":1048576,"modification":1700000000}]}`
	case path == "share_link/":
		body = `[{"token":"tok_abc","path":"` + enc("/Disque/zeta.txt") + `","name":"zeta.txt","expire":0,"fullurl":"https://example.org/share/tok_abc/"}]`
	case path == "fs/tasks/":
		body = `[{"id":7,"type":"cp","state":"running","error":"none","progress":42}]`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *filesBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	j, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(j))
	b.mu.Unlock()
	return nil
}

func (b *filesBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true}, nil
}

func (b *filesBox) last() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.writes) == 0 {
		return ""
	}
	return b.writes[len(b.writes)-1]
}

func filesSetup(t *testing.T) (*filesBox, *http.Client, string, func(string, url.Values) *http.Response) {
	t.Helper()
	box := &filesBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	return box, c, srv.URL, func(p string, f url.Values) *http.Response { return post(t, c, srv, p, f) }
}

func TestFilesPage(t *testing.T) {
	box := &filesBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/files?p="+url.QueryEscape(enc("/Disque")))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d\n%s", resp.StatusCode, body)
	}
	for _, want := range []string{"Photos/", "alpha.zip", "zeta.txt", "1.0 Mo", "Liens de partage", "tok_abc", "Tâches", "42 %"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// Folders first, then by name; hidden entries are left out.
	if i, j, k := strings.Index(body, "Photos/"), strings.Index(body, "alpha.zip"), strings.Index(body, ">zeta.txt"); i >= j || j >= k {
		t.Errorf("order wrong: %d %d %d", i, j, k)
	}
	if strings.Contains(body, `>./`) || strings.Contains(body, `aria-label="Sélectionner ."`) {
		t.Error("hidden entry shown")
	}
	// A bad path falls back to the root instead of reaching the box.
	if resp, _ := get(t, c, srv, "/files?p=!!"); resp.StatusCode != http.StatusOK {
		t.Errorf("bad path: %d", resp.StatusCode)
	}
}

func TestFilesWrites(t *testing.T) {
	box, _, _, send := filesSetup(t)
	cur := enc("/Disque")
	f1, f2 := enc("/Disque/a.txt"), enc("/Disque/b.txt")

	send("/files/mkdir", url.Values{"p": {cur}, "name": {"Nouveau"}})
	if got, want := box.last(), `POST fs/mkdir/ {"dirname":"Nouveau","parent":"`+cur+`"}`; got != want {
		t.Errorf("mkdir: %s", got)
	}
	send("/files/rename", url.Values{"p": {cur}, "src": {f1}, "name": {"c.txt"}})
	if got, want := box.last(), `POST fs/rename/ {"dst":"c.txt","src":"`+f1+`"}`; got != want {
		t.Errorf("rename: %s", got)
	}
	send("/files/move", url.Values{"p": {cur}, "files": {f1, f2}, "dest": {"/Disque/Photos"}, "mode": {"both"}})
	if got, want := box.last(), `POST fs/mv/ {"dst":"`+enc("/Disque/Photos")+`","files":["`+f1+`","`+f2+`"],"mode":"both"}`; got != want {
		t.Errorf("move: %s", got)
	}
	send("/files/copy", url.Values{"p": {cur}, "files": {f1}, "dest": {"/Disque/Photos"}, "mode": {"skip"}})
	if !strings.HasPrefix(box.last(), "POST fs/cp/ ") {
		t.Errorf("copy: %s", box.last())
	}
	send("/files/archive", url.Values{"p": {cur}, "files": {f1}, "archive": {"a.zip"}})
	if got, want := box.last(), `POST fs/archive/ {"dst":"`+enc("/Disque/a.zip")+`","files":["`+f1+`"]}`; got != want {
		t.Errorf("archive: %s", got)
	}
	send("/files/delete", url.Values{"p": {cur}, "files": {f1}, "confirm": {"1"}})
	if got, want := box.last(), `POST fs/rm/ {"files":["`+f1+`"]}`; got != want {
		t.Errorf("delete: %s", got)
	}
	send("/files/share", url.Values{"p": {cur}, "target": {f1}, "days": {"0"}, "confirm": {"1"}})
	if got, want := box.last(), `POST share_link/ {"expire":0,"fullurl":"","path":"`+f1+`"}`; got != want {
		t.Errorf("share: %s", got)
	}
	send("/files/share/tok_abc/delete", url.Values{"p": {cur}})
	if got, want := box.last(), `DELETE share_link/tok_abc null`; got != want {
		t.Errorf("share delete: %s", got)
	}
	send("/files/tasks/7/delete", url.Values{"p": {cur}})
	if got, want := box.last(), `DELETE fs/tasks/7 null`; got != want {
		t.Errorf("task delete: %s", got)
	}
}

func TestFilesDangerousNeedConfirm(t *testing.T) {
	box, _, _, send := filesSetup(t)
	cur, f := enc("/Disque"), enc("/Disque/a.txt")
	send("/files/delete", url.Values{"p": {cur}, "files": {f}})
	send("/files/share", url.Values{"p": {cur}, "target": {f}, "days": {"1"}})
	send("/files/move", url.Values{"p": {cur}, "files": {f}, "dest": {"/x"}, "mode": {"overwrite"}})
	if len(box.writes) != 0 {
		t.Fatalf("unconfirmed actions reached the box: %v", box.writes)
	}
}

func TestFilesRefuseBadInput(t *testing.T) {
	box, _, _, send := filesSetup(t)
	cur, f := enc("/Disque"), enc("/Disque/a.txt")
	send("/files/mkdir", url.Values{"p": {cur}, "name": {"../x"}})
	send("/files/rename", url.Values{"p": {cur}, "src": {f}, "name": {"a/b"}})
	send("/files/delete", url.Values{"p": {cur}, "files": {base64.StdEncoding.EncodeToString([]byte("relative"))}, "confirm": {"1"}})
	send("/files/move", url.Values{"p": {cur}, "files": {f}, "dest": {"/a/../b"}, "mode": {"both"}})
	send("/files/tasks/x/delete", url.Values{"p": {cur}})
	if len(box.writes) != 0 {
		t.Fatalf("bad input reached the box: %v", box.writes)
	}
}
