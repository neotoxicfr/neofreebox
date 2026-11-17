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

type playersBox struct {
	mu     sync.Mutex
	writes []string
}

func (b *playersBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "player/":
		body = `[{"id":11,"mac":"00:11:22:33:44:55","stb_type":"stb_v8","device_name":"Salon","device_model":"fbx8am","reachable":true,"api_available":true},
			{"mac":"00:11:22:33:44:66","device_name":"Chambre","reachable":true,"api_available":false}]`
	case "player/11/api/v6/status/":
		body = `{"power_state":"standby"}`
	case "player/11/api/v6/control/volume/":
		body = `{"mute":false,"volume":25}`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *playersBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	j, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(j))
	b.mu.Unlock()
	return nil
}

func (b *playersBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"player_control": true}, nil
}

func TestPlayersPage(t *testing.T) {
	srv := newTestServer(t, &playersBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/players")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"Salon", "en veille", `action="/players/11/control"`, `value="25"`, "Chambre", "ne relaie pas"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestPlayersWrites(t *testing.T) {
	box := &playersBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	post(t, c, srv, "/players/11/control", map[string][]string{"cmd": {"play_pause"}})
	post(t, c, srv, "/players/11/control", map[string][]string{"cmd": {"reboot"}})
	post(t, c, srv, "/players/x/control", map[string][]string{"cmd": {"stop"}})
	post(t, c, srv, "/players/11/volume", map[string][]string{"volume": {"60"}, "mute": {"1"}})
	post(t, c, srv, "/players/11/open", map[string][]string{"url": {"tv:?channel=2"}})
	post(t, c, srv, "/players/11/open", map[string][]string{"url": {"nonsense"}})
	want := []string{
		`POST player/11/api/v6/control/mediactrl/ {"cmd":"play_pause"}`,
		`PUT player/11/api/v6/control/volume/ {"mute":true,"volume":60}`,
		`POST player/11/api/v6/control/open {"url":"tv:?channel=2"}`,
	}
	if strings.Join(box.writes, "\n") != strings.Join(want, "\n") {
		t.Fatalf("writes:\n%s", strings.Join(box.writes, "\n"))
	}
}
