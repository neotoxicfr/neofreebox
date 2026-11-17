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

// dhcpBox serves anonymised DHCP answers and records the writes.
type dhcpBox struct {
	fakeBox
	mu     sync.Mutex
	writes []string // "METHOD path body"
}

func (b *dhcpBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "dhcp/config/":
		body = `{"enabled":true,"sticky_assign":true,"gateway":"192.168.1.254","netmask":"255.255.255.0","ip_range_start":"192.168.1.100","ip_range_end":"192.168.1.200","boot_server":"","boot_file":"","dns":["192.168.1.254","","","",""],"options":[]}`
	case "dhcp/static_lease/":
		body = `[{"id":"00:11:22:33:44:55","mac":"00:11:22:33:44:55","ip":"192.168.1.20","comment":"Imprimante","hostname":"printer","host":{"primary_name":"printer"}}]`
	case "dhcp/dynamic_lease/":
		body = `[{"mac":"00:11:22:33:44:66","ip":"192.168.1.101","hostname":"laptop","lease_remaining":3600,"assign_time":1700000000,"refresh_time":1700000100,"is_static":false,"host":{"primary_name":"laptop"}}]`
	case "dhcpv6/config/":
		body = `{"enabled":true,"use_custom_dns":false,"dns":["2001:db8::1"]}`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *dhcpBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	j, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(j))
	b.mu.Unlock()
	return nil
}

func dhcpSetup(t *testing.T) (*dhcpBox, *http.Client, string, func(string, url.Values) *http.Response) {
	t.Helper()
	box := &dhcpBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	return box, c, srv.URL, func(path string, f url.Values) *http.Response { return post(t, c, srv, path, f) }
}

func TestDHCPPage(t *testing.T) {
	srv := newTestServer(t, &dhcpBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/dhcp")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"192.168.1.100", "192.168.1.254", "Imprimante", "00:11:22:33:44:55", "laptop", "192.168.1.101", "2001:db8::1", "Désactiver le DHCP"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestDHCPWrites(t *testing.T) {
	box, _, _, send := dhcpSetup(t)
	send("/dhcp/static", url.Values{"mac": {"00:11:22:33:44:77"}, "ip": {"192.168.1.30"}, "comment": {"nas"}})
	send("/dhcp/static/00:11:22:33:44:55/update", url.Values{"ip": {"192.168.1.21"}, "comment": {"x"}})
	send("/dhcp/static/00:11:22:33:44:55/delete", nil)
	send("/dhcp/enabled", url.Values{"enabled": {"1"}})
	send("/dhcp/v6/dns", url.Values{"use_custom_dns": {"1"}})
	send("/dhcp/config", url.Values{"ip_range_start": {"192.168.1.10"}, "ip_range_end": {"192.168.1.20"}, "dns1": {"192.168.1.254"}, "sticky_assign": {"1"}})
	want := []string{
		`POST dhcp/static_lease/ {"comment":"nas","ip":"192.168.1.30","mac":"00:11:22:33:44:77"}`,
		`PUT dhcp/static_lease/00:11:22:33:44:55 {"comment":"x","ip":"192.168.1.21"}`,
		`DELETE dhcp/static_lease/00:11:22:33:44:55 null`,
		`PUT dhcp/config/ {"enabled":true}`,
		`PUT dhcpv6/config/ {"use_custom_dns":true}`,
		`PUT dhcp/config/ {"always_broadcast":false,"boot_file":"","boot_server":"","dns":["192.168.1.254"],"ignore_out_of_range_hint":false,"ip_range_end":"192.168.1.20","ip_range_start":"192.168.1.10","sticky_assign":true}`,
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

func TestDHCPInvalidInputSendsNothing(t *testing.T) {
	box, _, _, send := dhcpSetup(t)
	send("/dhcp/static", url.Values{"mac": {"nope"}, "ip": {"192.168.1.30"}})
	send("/dhcp/static", url.Values{"mac": {"00:11:22:33:44:77"}, "ip": {"2001:db8::1"}})
	send("/dhcp/config", url.Values{"ip_range_start": {"192.168.1.10"}, "ip_range_end": {"x"}})
	send("/dhcp/config", url.Values{"ip_range_start": {"192.168.1.10"}, "ip_range_end": {"192.168.1.20"}, "dns2": {"bad"}})
	if len(box.writes) != 0 {
		t.Fatalf("unexpected writes: %q", box.writes)
	}
}

func TestDHCPDisableNeedsConfirm(t *testing.T) {
	box, _, _, send := dhcpSetup(t)
	send("/dhcp/enabled", url.Values{"enabled": {"0"}})
	send("/dhcp/v6/enabled", url.Values{"enabled": {"0"}})
	if len(box.writes) != 0 {
		t.Fatalf("sent without confirm: %q", box.writes)
	}
	send("/dhcp/enabled", url.Values{"enabled": {"0"}, "confirm": {"1"}})
	send("/dhcp/v6/enabled", url.Values{"enabled": {"0"}, "confirm": {"1"}})
	if len(box.writes) != 2 || box.writes[0] != `PUT dhcp/config/ {"enabled":false}` || box.writes[1] != `PUT dhcpv6/config/ {"enabled":false}` {
		t.Fatalf("writes: %q", box.writes)
	}
}
