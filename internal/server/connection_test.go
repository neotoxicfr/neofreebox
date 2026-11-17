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

type connectionBox struct {
	fakeBox
	mu     sync.Mutex
	writes []string // "METHOD path body"
}

var connectionReplies = map[string]string{
	"connection/":                    `{"state":"up","type":"ethernet","media":"ftth","ipv4":"192.0.2.10","ipv6":"2001:db8::1","ipv4_port_range":[0,65535]}`,
	"connection/config/":             `{"ping":true,"wol":false,"adblock":false,"allow_token_request":true,"sip_alg":"disabled","remote_access":false,"remote_access_port":44666,"remote_access_min_port":0,"remote_access_max_port":65535,"remote_access_ip":"192.0.2.10","is_secure_pass":true}`,
	"connection/ipv6/config/":        `{"ipv6_enabled":true,"ipv6_firewall":true,"ipv6_prefix_firewall":false,"ipv6ll":"fe80::1","delegations":[{"prefix":"2001:db8:0:1::/64","next_hop":""},{"prefix":"2001:db8:0:2::/64","next_hop":"fe80::2"}]}`,
	"connection/ftth/":               `{"sfp_present":true,"sfp_has_signal":true,"sfp_has_power_report":true,"link":true,"sfp_alim_ok":true,"sfp_model":"MODEL-X","sfp_vendor":"Acme","sfp_serial":"SN0001","sfp_pwr_rx":-1669,"sfp_pwr_tx":270}`,
	"connection/lte/backup":          `{"state":"not_detected"}`,
	"connection/ddns/ovh/":           `{"enabled":true,"hostname":"home.example.org","user":"demo"}`,
	"connection/ddns/dyndns/":        `{"enabled":false,"hostname":"","user":""}`,
	"connection/ddns/noip/":          `{"enabled":false,"hostname":"","user":""}`,
	"connection/ddns/ovh/status/":    `{"status":"ok","last_refresh":1700000000}`,
	"connection/ddns/dyndns/status/": `{"status":"disabled"}`,
	"connection/ddns/noip/status/":   `{"status":"disabled"}`,
}

func (b *connectionBox) Get(_ context.Context, path string, out any) error {
	body, ok := connectionReplies[path]
	if !ok {
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *connectionBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	raw, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(raw))
	b.mu.Unlock()
	return nil
}

func connectionSetup(t *testing.T) (*connectionBox, string, func(string, url.Values) *http.Response, func(string) string) {
	t.Helper()
	box := &connectionBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	return box, srv.URL,
		func(p string, f url.Values) *http.Response { return post(t, c, srv, p, f) },
		func(p string) string {
			resp, body := get(t, c, srv, p)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: %d", p, resp.StatusCode)
			}
			return body
		}
}

func TestConnectionPage(t *testing.T) {
	_, _, _, page := connectionSetup(t)
	body := page("/connection")
	for _, want := range []string{"Fibre (FTTH)", "-16.69 dBm", "2.70 dBm", "SN0001", "home.example.org", "À jour", "2001:db8:0:2::/64", "fe80::2"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "Secours 4G") {
		t.Error("4G card shown without a module")
	}
}

func TestConnectionWrites(t *testing.T) {
	box, _, send, _ := connectionSetup(t)
	send("/connection/config", url.Values{"ping": {"1"}, "sip_alg": {"disabled"}})
	send("/connection/ddns/ovh", url.Values{"enabled": {"1"}, "hostname": {"h.example.org"}, "user": {"u"}, "password": {"pw"}})
	send("/connection/ddns/ovh", url.Values{"hostname": {"h.example.org"}, "user": {"u"}})
	send("/connection/remote", url.Values{"remote_access": {"1"}, "remote_access_port": {"44666"}, "confirm": {"1"}})
	send("/connection/ipv6", url.Values{"confirm": {"1"}, "ipv6_enabled": {"1"}, "n": {"1"}, "prefix_0": {"2001:db8:0:1::/64"}, "next_hop_0": {"fe80::9"}})
	want := []string{
		`PUT connection/config/ {"adblock":false,"allow_token_request":false,"ping":true,"sip_alg":"disabled","wol":false}`,
		`PUT connection/ddns/ovh/ {"enabled":true,"hostname":"h.example.org","password":"pw","user":"u"}`,
		`PUT connection/ddns/ovh/ {"enabled":false,"hostname":"h.example.org","user":"u"}`,
		`PUT connection/config/ {"remote_access":true,"remote_access_port":44666}`,
		`PUT connection/ipv6/config/ {"delegations":[{"next_hop":"fe80::9","prefix":"2001:db8:0:1::/64"}],"ipv6_enabled":true,"ipv6_firewall":false,"ipv6_prefix_firewall":false}`,
	}
	if strings.Join(box.writes, "\n") != strings.Join(want, "\n") {
		t.Fatalf("writes:\n%s\nwant:\n%s", strings.Join(box.writes, "\n"), strings.Join(want, "\n"))
	}
}

func TestConnectionDangerousNeedsConfirm(t *testing.T) {
	box, _, send, _ := connectionSetup(t)
	send("/connection/remote", url.Values{"remote_access": {"1"}, "remote_access_port": {"44666"}})
	send("/connection/ipv6", url.Values{"ipv6_enabled": {"1"}})
	send("/connection/ddns/unknown", url.Values{"enabled": {"1"}})
	send("/connection/config", url.Values{"sip_alg": {"bogus"}})
	if len(box.writes) != 0 {
		t.Fatalf("unexpected writes: %v", box.writes)
	}
	// Turning remote access off does not need the confirmation.
	send("/connection/remote", url.Values{"remote_access_port": {"44666"}})
	if len(box.writes) != 1 {
		t.Fatalf("disabling remote access: %v", box.writes)
	}
}
