package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type freeplugBox struct {
	fakeBox
	writes []string
}

func (b *freeplugBox) Get(_ context.Context, _ string, out any) error {
	return json.Unmarshal([]byte(`[{"id":"c8:00:00:00:00:01","members":[
	 {"id":"00:11:22:33:44:55","tx_rate":148,"rx_rate":147,"eth_port_status":"up","net_role":"sta","inactive":65,"model":"int6400","eth_speed":100,"local":true,"eth_full_duplex":true,"has_network":true},
	 {"id":"00:11:22:33:44:66","tx_rate":-1,"rx_rate":-1,"eth_port_status":"down","net_role":"cco","inactive":1,"model":"int6400","eth_speed":0,"local":false,"has_network":true}]}]`), out)
}

func (b *freeplugBox) Do(_ context.Context, method, path string, _, _ any) error {
	b.writes = append(b.writes, method+" "+path)
	return nil
}

func TestFreeplugPageAndReset(t *testing.T) {
	box := &freeplugBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))

	resp, body := get(t, c, srv, "/freeplug")
	for _, want := range []string{"00:11:22:33:44:55", "148 Mb/s", "Coordinateur central", "n/d", "branché à la box"} {
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("%d, %q missing", resp.StatusCode, want)
		}
	}

	post(t, c, srv, "/freeplug/00:11:22:33:44:55/reset", url.Values{})
	if len(box.writes) != 0 {
		t.Fatalf("reset without confirm sent %v", box.writes)
	}
	post(t, c, srv, "/freeplug/00:11:22:33:44:55/reset", url.Values{"confirm": {"1"}})
	if len(box.writes) != 1 || box.writes[0] != "POST freeplug/00:11:22:33:44:55/reset/" {
		t.Fatalf("writes: %v", box.writes)
	}
}
