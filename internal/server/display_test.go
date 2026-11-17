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

type displayBox struct {
	mu     sync.Mutex
	strip  bool
	writes []string
}

func (b *displayBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "lcd/config/":
		body = `{"hide_status_led":true,"hide_wifi_key":false,"brightness":40,"orientation":90,"orientation_forced":true}`
		if b.strip {
			body = `{"brightness":40,"orientation":0,"orientation_forced":false,"hide_wifi_key":false,"led_strip_enabled":true,"led_strip_brightness":70,
				"led_strip_animation":"organic","available_led_strip_animations":["organic","static"]}`
		}
	case "ledstrip/status":
		if !b.strip {
			return errors.New("not found")
		}
		body = `{"use_planning":false,"next_change":0}`
	case "ledstrip/planning/":
		body = `{"use_planning":false,"planning_mode":"ledstrip_off","resolution":2,"mapping":[false,true]}`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *displayBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	j, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(j))
	b.mu.Unlock()
	return nil
}

func (b *displayBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true}, nil
}

func TestDisplayPage(t *testing.T) {
	srv := newTestServer(t, &displayBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/display")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{`name="brightness"`, `value="40"`, "Masquer la clé Wi-Fi", "Éteindre le voyant de statut"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, absent := range []string{"Bande lumineuse", "Écran de veille", "Planning de la bande"} {
		if strings.Contains(body, absent) {
			t.Errorf("page shows %q, which this model lacks", absent)
		}
	}
}

func TestDisplaySendsOnlyKnownFields(t *testing.T) {
	box := &displayBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	post(t, c, srv, "/display", map[string][]string{"brightness": {"250"}, "orientation": {"180"}, "hide_status_led": {"on"}, "screensaver": {"on"}})
	want := `PUT lcd/config/ {"brightness":100,"hide_status_led":true,"hide_wifi_key":false,"orientation":180,"orientation_forced":false}`
	if len(box.writes) != 1 || box.writes[0] != want {
		t.Fatalf("writes: %v", box.writes)
	}
}

func TestDisplayLedStrip(t *testing.T) {
	box := &displayBox{strip: true}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	if _, body := get(t, c, srv, "/display"); !strings.Contains(body, "Planning de la bande") || !strings.Contains(body, "static") {
		t.Fatal("LED strip section missing")
	}
	post(t, c, srv, "/display/ledstrip-planning", map[string][]string{"use_planning": {"1"}})
	want := `PUT ledstrip/planning/ {"mapping":[false,true],"planning_mode":"ledstrip_off","resolution":2,"use_planning":true}`
	if len(box.writes) != 1 || box.writes[0] != want {
		t.Fatalf("writes: %v", box.writes)
	}
}
