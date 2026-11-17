package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

type diagnosticsBox struct {
	fakeBox
	writes []string
}

func (b *diagnosticsBox) Get(_ context.Context, _ string, out any) error {
	return json.Unmarshal([]byte(`[{"id":"11111111-2222-3333-4444-555555555555","name":"Phone","type":"ios","last_use":0,
	 "subscriptions":["security","phone"],"api_url":"https://notify.example.com/app","message_type":"notification"}]`), out)
}

func (b *diagnosticsBox) Do(_ context.Context, method, path string, _, _ any) error {
	b.writes = append(b.writes, method+" "+path)
	return nil
}

func TestNotifTargetWritesAreDangerous(t *testing.T) {
	for _, name := range []string{"notif_target_add", "notif_target_update"} {
		op, ok := ops.Get(name)
		if !ok || !op.Dangerous {
			t.Errorf("%s must be dangerous (alerts go to an arbitrary URL)", name)
		}
	}
}

func TestDiagnosticsPageAndDelete(t *testing.T) {
	box := &diagnosticsBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))

	resp, body := get(t, c, srv, "/diagnostics")
	for _, want := range []string{"Phone", "ios", "alarme, appels manqués", "jamais"} {
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("%d, %q missing", resp.StatusCode, want)
		}
	}
	path := "/diagnostics/targets/11111111-2222-3333-4444-555555555555/delete"
	post(t, c, srv, path, url.Values{})
	if len(box.writes) != 0 {
		t.Fatalf("delete without confirm sent %v", box.writes)
	}
	post(t, c, srv, path, url.Values{"confirm": {"1"}})
	if len(box.writes) != 1 || box.writes[0] != "DELETE notif/targets/11111111-2222-3333-4444-555555555555" {
		t.Fatalf("writes: %v", box.writes)
	}
}
