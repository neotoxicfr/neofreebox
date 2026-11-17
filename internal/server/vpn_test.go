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

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// vpnBox serves anonymised VPN answers and records the writes it receives.
type vpnBox struct {
	mu     sync.Mutex
	writes []string // "METHOD path body"
	gets   []string
}

const wgFile = "[Interface]\nPrivateKey = FAKEPRIVATEKEYFAKEPRIVATEKEYFAKEPRIVATEKEY0=\nAddress = 10.8.0.70/32\n\n[Peer]\nPublicKey = FAKEPUBLICKEY\nEndpoint = 192.0.2.10:51820\nAllowedIPs = 0.0.0.0/0\n"

var vpnAnswers = map[string]string{
	"vpn/": `[{"state":"started","type":"wireguard","name":"wireguard","connection_count":1,"auth_connection_count":1},
		{"state":"stopped","type":"openvpn","name":"openvpn_routed","connection_count":0,"auth_connection_count":0}]`,
	"vpn/wireguard/config/":      `{"enabled":true,"type":"wireguard","id":"wireguard","port":51820,"min_port":0,"max_port":65535,"conf_wireguard":{"mtu":1360},"ip_start":"10.8.0.65","ip_end":"10.8.0.95"}`,
	"vpn/openvpn_routed/config/": `{"enabled":false,"type":"openvpn","id":"openvpn_routed","port":1194,"conf_openvpn":{"cipher":"aes128"},"ip_start":"10.8.0.65","ip_end":"10.8.0.95"}`,
	"vpn/user/": `[{"type":"wireguard","login":"phone","ip_reservation":"10.8.0.70","conf_wireguard":{"keepalive":15,"psk":false},"password_set":false},
		{"type":"standard","login":"laptop","ip_reservation":"","password_set":true}]`,
	"vpn/connection/": `[{"id":"wireguard-1","vpn":"wireguard","user":"phone","authenticated":true,"auth_time":1700000000,"src_ip":"203.0.113.9","local_ip":"10.8.0.70","rx_bytes":2048,"tx_bytes":1024}]`,
	"vpn/ip_pool/":    `{"ip_start":"10.8.0.65","ip_end":"10.8.0.95","reservations":[{"login":"phone","ip":"10.8.0.70"}]}`,
}

func (b *vpnBox) Get(_ context.Context, path string, out any) error {
	b.mu.Lock()
	b.gets = append(b.gets, path)
	b.mu.Unlock()
	if path == "vpn/download_config/wireguard/phone/json" || path == "vpn/download_config/openvpn_routed/laptop/json" {
		b, _ := json.Marshal(wgFile)
		return json.Unmarshal(b, out)
	}
	body, ok := vpnAnswers[path]
	if !ok {
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *vpnBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	raw, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(raw))
	b.mu.Unlock()
	return nil
}

func (b *vpnBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true}, nil
}

func TestVPNPage(t *testing.T) {
	box := &vpnBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/vpn")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d\n%s", resp.StatusCode, body)
	}
	for _, want := range []string{"WireGuard", "OpenVPN (routé)", "en marche", "arrêté", "51820", "phone", "laptop", "10.8.0.70",
		"/vpn/config/wireguard/phone", "/vpn/qr/wireguard/phone", "203.0.113.9", "Fichier OpenVPN"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "PRIVATEKEY") {
		t.Error("page leaks a configuration")
	}
	if len(box.writes) != 0 {
		t.Fatalf("GET wrote: %v", box.writes)
	}
	for _, g := range box.gets {
		if strings.Contains(g, "download_config") {
			t.Errorf("page listing downloaded a configuration: %s", g)
		}
	}
}

func TestVPNWrites(t *testing.T) {
	box := &vpnBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	cases := []struct {
		path string
		form url.Values
		want string
	}{
		{"/vpn/openvpn_routed/start", url.Values{"confirm": {"1"}}, `PUT vpn/openvpn_routed/config/ {"enabled":true}`},
		{"/vpn/wireguard/stop", url.Values{"confirm": {"1"}}, `PUT vpn/wireguard/config/ {"enabled":false}`},
		{"/vpn/wireguard/port", url.Values{"confirm": {"1"}, "port": {"51821"}}, `PUT vpn/wireguard/config/ {"port":51821}`},
		{"/vpn/wireguard/settings", url.Values{"confirm": {"1"}, "type": {"wireguard"}, "mtu": {"1400"}}, `PUT vpn/wireguard/config/ {"conf_wireguard":{"mtu":1400}}`},
		{"/vpn/users", url.Values{"confirm": {"1"}, "login": {"tablet"}, "type": {"wireguard"}, "ip_reservation": {"10.8.0.71"}, "keepalive": {"15"}},
			`POST vpn/user/ {"conf_wireguard":{"keepalive":15,"psk":false},"ip_reservation":"10.8.0.71","login":"tablet","type":"wireguard"}`},
		{"/vpn/users/phone/update", url.Values{"confirm": {"1"}, "ip_reservation": {"10.8.0.72"}, "keepalive": {"25"}},
			`PUT vpn/user/phone {"conf_wireguard":{"keepalive":25,"psk":false},"ip_reservation":"10.8.0.72"}`},
		{"/vpn/users/phone/delete", url.Values{"confirm": {"1"}}, `DELETE vpn/user/phone null`},
		{"/vpn/connections/wireguard-1/close", url.Values{"confirm": {"1"}}, `DELETE vpn/connection/wireguard-1 null`},
	}
	for _, tc := range cases {
		box.writes = nil
		post(t, c, srv, tc.path, tc.form)
		if len(box.writes) != 1 || box.writes[0] != tc.want {
			t.Errorf("%s: wrote %v, want %q", tc.path, box.writes, tc.want)
		}
	}
}

func TestVPNDangerousNeedsConfirm(t *testing.T) {
	box := &vpnBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	for _, tc := range []struct {
		path string
		form url.Values
	}{
		{"/vpn/wireguard/stop", nil},
		{"/vpn/openvpn_routed/start", nil},
		{"/vpn/wireguard/settings", url.Values{"type": {"wireguard"}, "mtu": {"1400"}}},
		{"/vpn/users", url.Values{"login": {"tablet"}, "type": {"wireguard"}, "ip_reservation": {"10.8.0.71"}}},
		{"/vpn/users/phone/update", url.Values{"ip_reservation": {"10.8.0.72"}}},
		{"/vpn/wireguard/port", url.Values{"port": {"1234"}}},
		{"/vpn/users/phone/delete", nil},
		{"/vpn/connections/wireguard-1/close", nil},
	} {
		post(t, c, srv, tc.path, tc.form)
	}
	if len(box.writes) != 0 {
		t.Fatalf("unconfirmed actions wrote: %v", box.writes)
	}
	// An OpenVPN file download invalidates the old one: refuse GET and unconfirmed POST.
	if resp, _ := get(t, c, srv, "/vpn/config/openvpn_routed/laptop"); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET OpenVPN download: %d", resp.StatusCode)
	}
	post(t, c, srv, "/vpn/config/openvpn_routed/laptop", nil)
	for _, g := range box.gets {
		if strings.Contains(g, "download_config") {
			t.Errorf("unconfirmed download reached the box: %s", g)
		}
	}
}

func TestVPNDownloadAndQR(t *testing.T) {
	box := &vpnBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))

	resp, body := get(t, c, srv, "/vpn/config/wireguard/phone")
	if body != wgFile || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment;") ||
		resp.Header.Get("Cache-Control") != "no-store" || !strings.Contains(resp.Header.Get("Content-Disposition"), "wireguard_phone.conf") {
		t.Fatalf("download: %d %v", resp.StatusCode, resp.Header)
	}
	resp, body = get(t, c, srv, "/vpn/qr/wireguard/phone")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("Cache-Control") != "no-store" ||
		!strings.HasPrefix(body, "\x89PNG") {
		t.Fatalf("qr: %d %v", resp.StatusCode, resp.Header)
	}
	if resp, _ := get(t, c, srv, "/vpn/qr/openvpn_routed/laptop"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("QR for OpenVPN: %d", resp.StatusCode)
	}
	// Confirmed OpenVPN download.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/vpn/config/openvpn_routed/laptop", strings.NewReader("confirm=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	r2, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if !strings.Contains(r2.Header.Get("Content-Disposition"), "openvpn_routed_laptop.ovpn") {
		t.Errorf("OpenVPN download: %v", r2.Header)
	}
}

// The API and MCP refuse these operations without "confirm": true.
func TestMissionOpsNeedConfirm(t *testing.T) {
	for _, name := range []string{"vpn_user_add", "vpn_user_update", "vpn_config_download", "player_open", "storage_partition_check"} {
		op, ok := ops.Get(name)
		if !ok || !op.Dangerous {
			t.Errorf("%s: not dangerous", name)
			continue
		}
		if _, err := ops.Call(context.Background(), &vpnBox{}, op, map[string]any{}); !errors.Is(err, ops.ErrConfirm) {
			t.Errorf("%s without confirm: %v", name, err)
		}
	}
}

func TestVPNQRRequiresSignIn(t *testing.T) {
	srv := newTestServer(t, &vpnBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	post(t, c, srv, "/logout", nil)
	if resp, body := get(t, c, srv, "/vpn/qr/wireguard/phone"); resp.StatusCode == http.StatusOK || strings.HasPrefix(body, "\x89PNG") {
		t.Fatalf("QR served without sign-in: %d", resp.StatusCode)
	}
}
