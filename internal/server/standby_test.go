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

type standbyBox struct {
	mu     sync.Mutex
	writes []string
}

func (b *standbyBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "standby/status":
		body = `{"use_planning":false,"planning_mode":"wifi_off","available_planning_modes":["wifi_off"],"next_change":0}`
	case "standby/config/":
		m := make([]bool, 7*24)
		j, _ := json.Marshal(map[string]any{"use_planning": false, "planning_mode": "wifi_off", "resolution": 24, "mapping": m})
		body = string(j)
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *standbyBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	j, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(j))
	b.mu.Unlock()
	return nil
}

func (b *standbyBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true}, nil
}

func TestStandbyPage(t *testing.T) {
	srv := newTestServer(t, &standbyBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/standby")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"désactivé", "Couper le Wi-Fi", `aria-label="Lundi 00:00"`, `aria-label="Dimanche 23:00"`, `value="167"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestStandbySave(t *testing.T) {
	box := &standbyBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))

	// Active planning with a slot cuts the Wi-Fi: refused without confirm.
	form := map[string][]string{"use_planning": {"1"}, "planning_mode": {"wifi_off"}, "slot": {"1", "2", "999", "x"}}
	post(t, c, srv, "/standby", form)
	if len(box.writes) != 0 {
		t.Fatalf("unconfirmed save wrote: %v", box.writes)
	}
	form["confirm"] = []string{"1"}
	post(t, c, srv, "/standby", form)
	if len(box.writes) != 1 {
		t.Fatalf("writes: %v", box.writes)
	}
	w := box.writes[0]
	if !strings.HasPrefix(w, `PUT standby/config/ {"use_planning":true,"planning_mode":"wifi_off","resolution":24,"mapping":[false,true,true,false`) || strings.Count(w, "true") != 3 {
		t.Fatalf("body: %s", w)
	}

	// An inactive planning cuts nothing: no confirmation needed.
	post(t, c, srv, "/standby", map[string][]string{"planning_mode": {"bogus"}, "slot": {"3"}})
	if len(box.writes) != 2 || !strings.Contains(box.writes[1], `"use_planning":false,"planning_mode":"wifi_off"`) {
		t.Fatalf("writes: %v", box.writes)
	}
}
