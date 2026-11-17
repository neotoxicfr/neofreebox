package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// statusBox adds fans and the figures of the "En bref" card to fakeBox.
type statusBox struct{ fakeBox }

func (b statusBox) Get(ctx context.Context, path string, out any) error {
	var body string
	switch path {
	case "system/":
		body = `{"firmware_version":"4.13.5","uptime_val":90061,"model_info":{"pretty_name":"Freebox"},"sensors":[],"fans":[{"name":"Fan","value":2000}]}`
	case "lan/browser/pub/":
		body = `[{"id":"a","active":true},{"id":"b","active":false},{"id":"c","active":true}]`
	case "call/log/":
		body = `[{"new":true},{"new":false}]`
	case "wifi/config/":
		body = `{"enabled":true}`
	default:
		return b.fakeBox.Get(ctx, path, out)
	}
	return json.Unmarshal([]byte(body), out)
}

func statusBody(t *testing.T, box Box) string {
	t.Helper()
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	_, body := get(t, c, srv, "/")
	return body
}

func TestStatusBriefAndLabels(t *testing.T) {
	body := statusBody(t, statusBox{})
	for _, want := range []string{"En bref", "2 sur 3 vus par la box", "Appels non lus", "Fibre (FTTH)", "Ventilateur 1", `class="dot up" aria-hidden="true"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, ">ftth<") {
		t.Error("raw media shown")
	}
}

func TestStatusBriefIsOptional(t *testing.T) {
	body := statusBody(t, fakeBox{})
	if !strings.Contains(body, "En bref") || strings.Contains(body, "Appels non lus") {
		t.Error("the card must show without the optional lines")
	}
}
