package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type switchBox struct {
	fakeBox
	writes []string // "METHOD path body"
}

func (b *switchBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "switch/status/":
		body = `[{"id":2,"name":"Ethernet 2","link":"down","speed":"10","duplex":"half","mode":"10BaseT-HD"},
		{"id":1,"name":"Ethernet 1","link":"up","speed":"1000","duplex":"full","mode":"1000BaseT-FD","mac_list":[{"mac":"00:11:22:33:44:55","hostname":"demo-host"}]}]`
	case "switch/port/1":
		body = `{"id":1,"speed":"auto","duplex":"auto"}`
	case "switch/port/2":
		body = `{"id":2,"speed":"100","duplex":"full"}`
	case "switch/port/1/stats", "switch/port/2/stats":
		body = `{"rx_good_bytes":2048,"tx_bytes":1048576,"rx_bytes_rate":100,"tx_bytes_rate":200,"rx_err_packets":1,"rx_fcs_packets":2,"tx_collisions":0}`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *switchBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	raw, _ := json.Marshal(in)
	b.writes = append(b.writes, method+" "+path+" "+string(raw))
	return nil
}

func TestSwitch(t *testing.T) {
	box := &switchBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))

	resp, body := get(t, c, srv, "/switch")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /switch: %d", resp.StatusCode)
	}
	for _, want := range []string{"Ethernet 1", "demo-host", "1000BaseT-FD", "2.0 Ko", "1.0 Mo", `<option value="100" selected>`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Index(body, "Ethernet 1") > strings.Index(body, "Ethernet 2") {
		t.Error("ports not sorted by id")
	}

	// Without the confirmation, or with a bad value, nothing is sent.
	post(t, c, srv, "/switch/1", url.Values{"speed": {"10"}, "duplex": {"auto"}})
	post(t, c, srv, "/switch/1", url.Values{"speed": {"9999"}, "duplex": {"auto"}, "confirm": {"1"}})
	if len(box.writes) != 0 {
		t.Fatalf("unexpected writes: %v", box.writes)
	}
	post(t, c, srv, "/switch/1", url.Values{"speed": {"100"}, "duplex": {"full"}, "confirm": {"1"}})
	if want := `PUT switch/port/1 {"duplex":"full","speed":"100"}`; len(box.writes) != 1 || box.writes[0] != want {
		t.Fatalf("writes: %v, want %s", box.writes, want)
	}
}
