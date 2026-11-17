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

type vpnClientBox struct {
	mu     sync.Mutex
	writes []string
}

var vpnClientAnswers = map[string]string{
	"vpn_client/config/": `[{"id":"vpn0","type":"wireguard","description":"office","active":false,
		"conf_wireguard":{"remote_addr":"192.0.2.1","remote_port":51820,"local_priv_key":"SECRETKEYSECRETKEY=","remote_public_key":"PUB=","local_addr":[{"ip":"198.51.100.10","len":24}]}},
		{"id":"vpn1","type":"pptp","description":"old","active":true,"conf_pptp":{"remote_host":"vpn.example.org","username":"user1","mppe":"require"}}]`,
	"vpn_client/status": `{"enabled":true,"active_vpn":"vpn1","active_vpn_description":"old","type":"pptp","state":"up","last_error":"none","last_up":1700000000,
		"stats":{"rate_up":0,"rate_down":0,"bytes_up":94,"bytes_down":94},"ipv4":{"ip_mask":{"ip":"198.51.100.20","mask":"255.255.255.255"},"gateway":"198.51.100.1","dns":["198.51.100.53"]}}`,
	"vpn_client/log": `"2024-01-01 00:00:00 dbg: ppp: connected"`,
}

func (b *vpnClientBox) Get(_ context.Context, path string, out any) error {
	body, ok := vpnClientAnswers[path]
	if !ok {
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *vpnClientBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	raw, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(raw))
	b.mu.Unlock()
	return nil
}

func (b *vpnClientBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true}, nil
}

func TestVPNClientPage(t *testing.T) {
	box := &vpnClientBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/vpn-client")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d\n%s", resp.StatusCode, body)
	}
	for _, want := range []string{"office", "192.0.2.1:51820", "vpn.example.org", "connecté", "198.51.100.20", "ppp: connected", "active"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "SECRETKEY") {
		t.Error("page leaks a private key")
	}
	if len(box.writes) != 0 {
		t.Fatalf("GET wrote: %v", box.writes)
	}
}

func TestVPNClientWrites(t *testing.T) {
	box := &vpnClientBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	conf := url.Values{"confirm": {"1"}}
	cases := []struct {
		path string
		form url.Values
		want string
	}{
		{"/vpn-client/vpn0/activate", conf, `PUT vpn_client/config/vpn0 {"active":true}`},
		{"/vpn-client/vpn1/deactivate", nil, `PUT vpn_client/config/vpn1 {"active":false}`},
		{"/vpn-client/vpn0/rename", url.Values{"description": {"bureau"}}, `PUT vpn_client/config/vpn0 {"description":"bureau"}`},
		{"/vpn-client/vpn0/delete", conf, `DELETE vpn_client/config/vpn0 null`},
		{"/vpn-client/add", url.Values{"type": {"pptp"}, "description": {"x"}, "remote_host": {"vpn.example.org"}, "username": {"u"}, "password": {"p"}, "mppe": {"require"}},
			`POST vpn_client/config/ {"active":false,"conf_pptp":{"allowed_auth":{"mschapv2":true},"mppe":"require","password":"p","remote_host":"vpn.example.org","username":"u"},"description":"x","type":"pptp"}`},
		{"/vpn-client/add", url.Values{"type": {"wireguard"}, "description": {"w"}, "remote_addr": {"192.0.2.1"}, "remote_port": {"51820"},
			"remote_public_key": {"PUB="}, "local_priv_key": {"PRIV="}, "local_ip": {"10.0.0.2"}, "local_len": {"32"}, "dns": {"10.0.0.1"}},
			`POST vpn_client/config/ {"active":false,"conf_wireguard":{"dns":["10.0.0.1"],"local_addr":[{"ip":"10.0.0.2","len":32}],"local_priv_key":"PRIV=","remote_addr":"192.0.2.1","remote_port":51820,"remote_preshared_key":"","remote_public_key":"PUB="},"description":"w","type":"wireguard"}`},
	}
	for _, tc := range cases {
		box.writes = nil
		post(t, c, srv, tc.path, tc.form)
		if len(box.writes) != 1 || box.writes[0] != tc.want {
			t.Errorf("%s: wrote %v, want %q", tc.path, box.writes, tc.want)
		}
	}
}

func TestVPNClientDangerousNeedsConfirm(t *testing.T) {
	box := &vpnClientBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	post(t, c, srv, "/vpn-client/vpn0/activate", nil)
	post(t, c, srv, "/vpn-client/vpn0/delete", nil)
	if len(box.writes) != 0 {
		t.Fatalf("unconfirmed actions wrote: %v", box.writes)
	}
}
