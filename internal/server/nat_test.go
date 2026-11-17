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

// natBox serves anonymised NAT answers and records the writes.
type natBox struct {
	fakeBox
	mu     sync.Mutex
	writes []string // "METHOD path body"
}

func (b *natBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "fw/redir/":
		body = `[{"id":1,"enabled":true,"comment":"web","ip_proto":"tcp","wan_port_start":8080,"wan_port_end":8082,"lan_ip":"192.168.1.42","lan_port":80,"src_ip":"0.0.0.0","hostname":"server"},
			{"id":2,"enabled":false,"comment":"game","ip_proto":"udp","wan_port_start":27015,"wan_port_end":27015,"lan_ip":"192.168.1.43","lan_port":27015,"src_ip":"192.0.2.9","hostname":"console"}]`
	case "fw/dmz/":
		body = `{"enabled":false,"ip":""}`
	case "fw/incoming/":
		body = `[{"id":"https","enabled":false,"active":false,"type":"tcp","in_port":443,"min_port":0,"max_port":65535,"readonly":false,"netns":"init"},
			{"id":"bittorrent-main","enabled":true,"active":true,"type":"tcp","in_port":17000,"min_port":0,"max_port":65535,"readonly":false,"netns":"vpn"}]`
	case "upnpigd/config/":
		body = `{"enabled":true,"version":2}`
	case "upnpigd/redir/":
		body = `[{"id":"0.0.0.0-53644-udp","enabled":true,"proto":"udp","desc":"some-app","remaining":0,"ext_src_ip":"0.0.0.0","int_port":16402,"int_ip":"192.168.1.44","ext_port":53644}]`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *natBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	j, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(j))
	b.mu.Unlock()
	return nil
}

func natSetup(t *testing.T) (*natBox, func(string, url.Values)) {
	t.Helper()
	box := &natBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	return box, func(path string, f url.Values) { post(t, c, srv, path, f) }
}

func TestNATPage(t *testing.T) {
	srv := newTestServer(t, &natBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/nat")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"8080–8082", "192.168.1.42:80–82", "27015", "192.0.2.9", "DMZ", "Accès distant Freebox OS (HTTPS)", "BitTorrent (principal)", "some-app", "192.168.1.44:16402"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestNATWrites(t *testing.T) {
	box, send := natSetup(t)
	send("/nat/redir", url.Values{"ip_proto": {"tcp"}, "wan_port_start": {"8443"}, "lan_ip": {"192.168.1.42"}, "lan_port": {"443"}, "comment": {"x"}, "confirm": {"1"}})
	send("/nat/redir/2/enabled", url.Values{"enabled": {"1"}, "confirm": {"1"}})
	send("/nat/redir/1/enabled", url.Values{"enabled": {"0"}})
	send("/nat/redir/1/delete", url.Values{"confirm": {"1"}})
	send("/nat/dmz", url.Values{"enabled": {"1"}, "ip": {"192.168.1.42"}, "confirm": {"1"}})
	send("/nat/dmz", url.Values{"enabled": {"0"}})
	send("/nat/incoming/https", url.Values{"enabled": {"1"}, "in_port": {"4443"}, "confirm": {"1"}})
	send("/nat/incoming/https", url.Values{"enabled": {"0"}})
	send("/nat/upnp", url.Values{"enabled": {"1"}, "version": {"1"}, "confirm": {"1"}})
	send("/nat/upnp/redir/0.0.0.0-53644-udp/delete", url.Values{"confirm": {"1"}})
	want := []string{
		`POST fw/redir/ {"comment":"x","enabled":true,"ip_proto":"tcp","lan_ip":"192.168.1.42","lan_port":443,"src_ip":"0.0.0.0","wan_port_end":8443,"wan_port_start":8443}`,
		`PUT fw/redir/2 {"enabled":true}`,
		`PUT fw/redir/1 {"enabled":false}`,
		`DELETE fw/redir/1 null`,
		`PUT fw/dmz/ {"enabled":true,"ip":"192.168.1.42"}`,
		`PUT fw/dmz/ {"enabled":false}`,
		`PUT fw/incoming/https {"enabled":true,"in_port":4443}`,
		`PUT fw/incoming/https {"enabled":false}`,
		`PUT upnpigd/config/ {"enabled":true,"version":1}`,
		`DELETE upnpigd/redir/0.0.0.0-53644-udp null`,
	}
	if len(box.writes) != len(want) {
		t.Fatalf("writes: %q", box.writes)
	}
	for i, w := range want {
		if box.writes[i] != w {
			t.Errorf("write %d:\n got %s\nwant %s", i, box.writes[i], w)
		}
	}
}

func TestNATExposureNeedsConfirm(t *testing.T) {
	box, send := natSetup(t)
	send("/nat/redir", url.Values{"ip_proto": {"tcp"}, "wan_port_start": {"22"}, "lan_ip": {"192.168.1.42"}, "lan_port": {"22"}})
	send("/nat/redir/2/enabled", url.Values{"enabled": {"1"}})
	send("/nat/dmz", url.Values{"enabled": {"1"}, "ip": {"192.168.1.42"}})
	send("/nat/incoming/https", url.Values{"enabled": {"1"}})
	send("/nat/upnp", url.Values{"enabled": {"1"}, "version": {"2"}})
	send("/nat/redir/1/delete", nil)
	send("/nat/upnp/redir/0.0.0.0-53644-udp/delete", nil)
	if len(box.writes) != 0 {
		t.Fatalf("sent without confirm: %q", box.writes)
	}
}

func TestNATInvalidInputSendsNothing(t *testing.T) {
	box, send := natSetup(t)
	send("/nat/redir", url.Values{"ip_proto": {"icmp"}, "wan_port_start": {"80"}, "lan_ip": {"192.168.1.42"}, "lan_port": {"80"}, "confirm": {"1"}})
	send("/nat/redir", url.Values{"ip_proto": {"tcp"}, "wan_port_start": {"90"}, "wan_port_end": {"80"}, "lan_ip": {"192.168.1.42"}, "lan_port": {"80"}, "confirm": {"1"}})
	send("/nat/redir", url.Values{"ip_proto": {"tcp"}, "wan_port_start": {"80"}, "lan_ip": {"nope"}, "lan_port": {"80"}, "confirm": {"1"}})
	send("/nat/dmz", url.Values{"enabled": {"1"}, "ip": {"nope"}, "confirm": {"1"}})
	if len(box.writes) != 0 {
		t.Fatalf("unexpected writes: %q", box.writes)
	}
}
