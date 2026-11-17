package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// wifiWrite is one write the fake box received.
type wifiWrite struct {
	Method, Path string
	Body         string
}

// wifiBox serves anonymised Wi-Fi answers and records the writes.
type wifiBox struct {
	filter string // mac_filter_state served
	white  bool   // a whitelist entry exists
	writes *[]wifiWrite
}

const wifiPlanJSON = `{"use_planning":false,"resolution":24,"mapping":[` +
	`"on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on",` +
	`"on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on",` +
	`"on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on",` +
	`"on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on",` +
	`"on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on",` +
	`"on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on",` +
	`"on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","on","off"]}`

func (b wifiBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "wifi/config/":
		body = `{"enabled":true,"power_saving":true,"mac_filter_state":"` + b.filter + `"}`
	case "wifi/state/":
		body = `{"state":"enabled","expected_phys":[{"band":"2d4g","phy_id":0,"detected":true}]}`
	case "wifi/steering/config/":
		body = `{"steering_level":1}`
	case "wifi/temp_disable":
		body = `{"remaining":0}`
	case "wifi/ap/":
		body = `[{"id":0,"name":"2.4G","config":{"band":"2d4g","channel_width":"40","primary_channel":0,"secondary_channel":0,"dfs_enabled":false},` +
			`"status":{"state":"active","channel_width":"20","primary_channel":6,"secondary_channel":0}}]`
	case "wifi/bss/":
		body = `[{"id":"00:11:22:33:44:55","phy_id":0,"use_shared_params":true,"config":{"enabled":true,"ssid":"Maison","encryption":"wpa2_psk_ccmp",` +
			`"hide_ssid":false,"key":"cle-de-test-123","wps_enabled":true},"status":{"state":"active","sta_count":2}}]`
	case "wifi/diag":
		body = `{"aps":[{"severity":"major","ap_id":0,"code":"channel_width"}],"bsss":[{"severity":"minor","bssid":"00:11:22:33:44:55","code":"network_security"}]}`
	case "wifi/planning/":
		body = wifiPlanJSON
	case "wifi/mac_filter/":
		typ := "blacklist"
		if b.white {
			typ = "whitelist"
		}
		body = `[{"id":"00:11:22:33:44:66","mac":"00:11:22:33:44:66","type":"` + typ + `","comment":"tablette","hostname":"tablette-test"}]`
	case "wifi/custom_keys/config/":
		body = `{"ssid":"Invites","ssid_read_only":false,"hide_ssid":false,"encryption":"wpa2_psk_ccmp"}`
	case "wifi/custom_key/":
		body = `[{"id":8,"remaining":3600,"params":{"description":"soiree","key":"invite-key-456","max_use_count":10,"duration":86400,"access_type":"net_only"}}]`
	case "wifi/wps/config/":
		body = `{"enabled":true}`
	case "wifi/wps/sessions/":
		body = `[{"id":1,"ssid":"Maison","active":false,"result":"failed_timeout","start_date":1700000000,"end_date":1700000120,"mac":"00:00:00:00:00:00"}]`
	case "wifi/ap/0/stations/":
		body = `[{"mac":"00:11:22:33:44:77","hostname":"telephone-test","state":"authenticated","signal":-55,"conn_duration":120,"rx_bytes":2048,"tx_bytes":4096,"last_tx":{"bitrate":722}}]`
	case "wifi/ap/0/neighbors/":
		body = `[{"ssid":"Voisin","bssid":"00:11:22:33:44:88","band":"2d4g","channel":1,"secondary_channel":0,"channel_width":"20","signal":-70}]`
	case "wifi/ap/0/channel_usage/":
		body = `[{"band":"2d4g","channel":1,"noise_level":-90,"rx_busy_percent":14}]`
	case "wifi/ap/0/allowed_channel_comb":
		body = `[{"band":"2d4g","channel_width":"20","need_dfs":false,"primary":1,"secondary":0},{"band":"2d4g","channel_width":"40","need_dfs":false,"primary":1,"secondary":5}]`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b wifiBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	raw, _ := json.Marshal(in)
	*b.writes = append(*b.writes, wifiWrite{method, path, string(raw)})
	return nil
}

func (wifiBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true}, nil
}

func wifiSession(t *testing.T, box wifiBox) (*httptest.Server, *http.Client, *[]wifiWrite) {
	t.Helper()
	var writes []wifiWrite
	box.writes = &writes
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	return srv, c, &writes
}

func TestWifiPagesRender(t *testing.T) {
	srv, c, _ := wifiSession(t, wifiBox{filter: "disabled"})
	for path, want := range map[string][]string{
		"/wifi":          {"Réseaux Wi-Fi", "Maison", "WPA2 (AES)", "2.4G", "canal 6", "cle-de-test-123", "sécurité faible", "Couper le Wi-Fi"},
		"/wifi-guest":    {"Accès invités", "Invites", "soiree", "invite-key-456", "internet seulement"},
		"/wifi-planning": {"Planning Wi-Fi", "Lundi", `name="slot"`, "60 min"},
		"/wifi-filter":   {"Filtre MAC", "00:11:22:33:44:66", "tablette", "désactivé"},
		"/wifi-wps":      {"WPS", "Maison", "aucun appareil"},
		"/wifi-radar":    {"telephone-test", "-55 dBm", "72 Mb/s", "Voisin", "14 %", "1&#43;5"},
	} {
		resp, body := get(t, c, srv, path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: %q missing", path, w)
			}
		}
	}
}

func TestWifiBoxDown(t *testing.T) {
	srv := newTestServer(t, fakeBox{err: errors.New("dial tcp: timeout")})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	for _, p := range []string{"/wifi", "/wifi-guest", "/wifi-planning", "/wifi-filter", "/wifi-wps", "/wifi-radar"} {
		if resp, _ := get(t, c, srv, p); resp.StatusCode != http.StatusBadGateway {
			t.Errorf("%s with box down: %d", p, resp.StatusCode)
		}
	}
}

func lastWrite(t *testing.T, w *[]wifiWrite) wifiWrite {
	t.Helper()
	if len(*w) == 0 {
		t.Fatal("no write sent to the box")
	}
	return (*w)[len(*w)-1]
}

func TestWifiWrites(t *testing.T) {
	ok := url.Values{"confirm": {"1"}}
	with := func(kv ...string) url.Values {
		v := url.Values{"confirm": {"1"}}
		for i := 0; i < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}
	tests := []struct {
		name, path string
		form       url.Values
		method     string
		box        string
		body       string
		wantBox    wifiBox
	}{
		{"power", "/wifi/power", url.Values{"power_saving": {"1"}}, "PUT", "wifi/config/", `{"power_saving":true}`, wifiBox{}},
		{"wifi on", "/wifi/enabled", url.Values{"enabled": {"1"}}, "PUT", "wifi/config/", `{"enabled":true}`, wifiBox{}},
		{"wifi off", "/wifi/enabled", with("enabled", "0"), "PUT", "wifi/config/", `{"enabled":false}`, wifiBox{}},
		{"steering", "/wifi/steering", url.Values{"steering_level": {"2"}}, "PUT", "wifi/steering/config/", `{"steering_level":2}`, wifiBox{}},
		{"temp disable", "/wifi/temp-disable", with("minutes", "20", "keep", "2d4g"), "POST", "wifi/temp_disable", `{"duration":1200,"keep":"2d4g"}`, wifiBox{}},
		{"ap edit", "/wifi/ap/0", with("channel_width", "20", "primary_channel", "6", "secondary_channel", "0"), "PUT", "wifi/ap/0",
			`{"config":{"channel_width":"20","dfs_enabled":false,"primary_channel":6,"secondary_channel":0}}`, wifiBox{}},
		{"ap restart", "/wifi/ap/0/restart", ok, "POST", "wifi/ap/0/restart", `null`, wifiBox{}},
		{"bss edit", "/wifi/bss/00:11:22:33:44:55", with("ssid", "Maison2", "encryption", "wpa3_psk_ccmp", "key", "nouvelle-cle-123", "enabled", "1"), "PUT",
			"wifi/bss/00:11:22:33:44:55", `{"config":{"enabled":true,"encryption":"wpa3_psk_ccmp","hide_ssid":false,"key":"nouvelle-cle-123","ssid":"Maison2"}}`, wifiBox{}},
		{"bss edit keeps key", "/wifi/bss/00:11:22:33:44:55", with("ssid", "Maison", "encryption", "wpa2_psk_ccmp"), "PUT",
			"wifi/bss/00:11:22:33:44:55", `{"config":{"enabled":false,"encryption":"wpa2_psk_ccmp","hide_ssid":false,"ssid":"Maison"}}`, wifiBox{}},
		{"diag fix ap", "/wifi/diag/fix", with("target", "ap:0", "code", "channel_width"), "POST", "wifi/diag", `{"aps":[{"ap_id":0,"code":"channel_width"}]}`, wifiBox{}},
		{"diag fix bss", "/wifi/diag/fix", with("target", "bss:00:11:22:33:44:55", "code", "network_security"), "POST", "wifi/diag",
			`{"bsss":[{"bssid":"00:11:22:33:44:55","code":"network_security"}]}`, wifiBox{}},
		{"reset", "/wifi/reset", ok, "POST", "wifi/config/reset/", `null`, wifiBox{}},
		{"guest rename", "/wifi-guest/config", url.Values{"ssid": {"Visiteurs"}}, "PUT", "wifi/custom_keys/config/", `{"ssid":"Visiteurs"}`, wifiBox{}},
		{"guest add", "/wifi-guest/keys", url.Values{"description": {"amis"}, "key": {"une-cle-longue"}, "max_use_count": {"5"}, "duration": {"3600"}, "access_type": {"net_only"}},
			"POST", "wifi/custom_key/", `{"access_type":"net_only","description":"amis","duration":3600,"key":"une-cle-longue","max_use_count":5}`, wifiBox{}},
		{"guest delete", "/wifi-guest/keys/8/delete", ok, "DELETE", "wifi/custom_key/8", `null`, wifiBox{}},
		{"filter state", "/wifi-filter/state", with("mac_filter_state", "blacklist"), "PUT", "wifi/config/", `{"mac_filter_state":"blacklist"}`, wifiBox{filter: "disabled"}},
		{"filter state whitelist", "/wifi-filter/state", with("mac_filter_state", "whitelist"), "PUT", "wifi/config/", `{"mac_filter_state":"whitelist"}`, wifiBox{filter: "disabled", white: true}},
		{"filter off", "/wifi-filter/state", url.Values{"mac_filter_state": {"disabled"}}, "PUT", "wifi/config/", `{"mac_filter_state":"disabled"}`, wifiBox{filter: "whitelist"}},
		{"filter add inactive", "/wifi-filter/entries", url.Values{"mac": {"00:11:22:33:44:99"}, "type": {"blacklist"}, "comment": {"x"}}, "POST", "wifi/mac_filter/",
			`{"comment":"x","mac":"00:11:22:33:44:99","type":"blacklist"}`, wifiBox{filter: "disabled"}},
		{"filter add active", "/wifi-filter/entries", with("mac", "00:11:22:33:44:aa", "type", "whitelist"), "POST", "wifi/mac_filter/",
			`{"comment":"","mac":"00:11:22:33:44:AA","type":"whitelist"}`, wifiBox{filter: "whitelist"}},
		{"filter edit", "/wifi-filter/entries/00:11:22:33:44:66", with("type", "whitelist", "comment", "ok"), "PUT", "wifi/mac_filter/00:11:22:33:44:66",
			`{"comment":"ok","type":"whitelist"}`, wifiBox{filter: "blacklist"}},
		{"filter delete", "/wifi-filter/entries/00:11:22:33:44:66/delete", ok, "DELETE", "wifi/mac_filter/00:11:22:33:44:66", `null`, wifiBox{filter: "blacklist"}},
		{"planning", "/wifi-planning", with("use_planning", "1", "preset", "all"), "PUT", "wifi/planning/", "", wifiBox{}},
		{"wps off", "/wifi-wps/config", url.Values{"enabled": {"0"}}, "PUT", "wifi/wps/config/", `{"enabled":false}`, wifiBox{}},
		{"wps on", "/wifi-wps/config", with("enabled", "1"), "PUT", "wifi/wps/config/", `{"enabled":true}`, wifiBox{}},
		{"wps start", "/wifi-wps/start", with("bssid", "00:11:22:33:44:55"), "POST", "wifi/wps/start/", `{"bssid":"00:11:22:33:44:55"}`, wifiBox{}},
		{"wps stop", "/wifi-wps/stop", url.Values{"session_id": {"3"}}, "POST", "wifi/wps/stop/", `{"session_id":3}`, wifiBox{}},
		{"wps clear", "/wifi-wps/clear", nil, "DELETE", "wifi/wps/sessions/", `null`, wifiBox{}},
		{"scan", "/wifi-radar/scan", with("ap", "0"), "POST", "wifi/ap/0/neighbors/scan", `null`, wifiBox{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, c, writes := wifiSession(t, tc.wantBox)
			resp := post(t, c, srv, tc.path, tc.form)
			if resp.StatusCode != http.StatusSeeOther {
				t.Fatalf("status %d", resp.StatusCode)
			}
			w := lastWrite(t, writes)
			if w.Method != tc.method || w.Path != tc.box {
				t.Fatalf("sent %s %s, want %s %s", w.Method, w.Path, tc.method, tc.box)
			}
			if tc.body != "" && w.Body != tc.body {
				t.Fatalf("body %s, want %s", w.Body, tc.body)
			}
		})
	}
}

func TestWifiPlanningMapping(t *testing.T) {
	srv, c, writes := wifiSession(t, wifiBox{})
	post(t, c, srv, "/wifi-planning", url.Values{"use_planning": {"1"}, "preset": {"night"}, "confirm": {"1"}})
	var body struct {
		Use     bool     `json:"use_planning"`
		Mapping []string `json:"mapping"`
	}
	if err := json.Unmarshal([]byte(lastWrite(t, writes).Body), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Use || len(body.Mapping) != 7*24 || body.Mapping[0] != "off" || body.Mapping[5] != "off" || body.Mapping[6] != "on" || body.Mapping[24] != "off" {
		t.Fatalf("night preset mapping wrong: %v", body.Mapping[:30])
	}
}

func TestWifiDangerousNeedsConfirm(t *testing.T) {
	cases := []struct {
		name, path string
		form       url.Values
		box        wifiBox
	}{
		{"wifi off", "/wifi/enabled", url.Values{"enabled": {"0"}}, wifiBox{}},
		{"temp disable", "/wifi/temp-disable", url.Values{"minutes": {"20"}, "keep": {"5g"}}, wifiBox{}},
		{"ap edit", "/wifi/ap/0", url.Values{"channel_width": {"20"}}, wifiBox{}},
		{"ap restart", "/wifi/ap/0/restart", nil, wifiBox{}},
		{"bss edit", "/wifi/bss/00:11:22:33:44:55", url.Values{"ssid": {"X"}, "encryption": {"wpa2_psk_ccmp"}, "key": {"12345678"}}, wifiBox{}},
		{"diag fix", "/wifi/diag/fix", url.Values{"target": {"ap:0"}, "code": {"all"}}, wifiBox{}},
		{"reset", "/wifi/reset", nil, wifiBox{}},
		{"guest delete", "/wifi-guest/keys/8/delete", nil, wifiBox{}},
		{"guest full access", "/wifi-guest/keys", url.Values{"access_type": {"full"}, "max_use_count": {"1"}}, wifiBox{}},
		{"filter on", "/wifi-filter/state", url.Values{"mac_filter_state": {"blacklist"}}, wifiBox{filter: "disabled"}},
		{"filter add while active", "/wifi-filter/entries", url.Values{"mac": {"00:11:22:33:44:99"}, "type": {"blacklist"}}, wifiBox{filter: "blacklist"}},
		{"filter delete while active", "/wifi-filter/entries/00:11:22:33:44:66/delete", nil, wifiBox{filter: "whitelist"}},
		{"planning cut", "/wifi-planning", url.Values{"use_planning": {"1"}, "preset": {"night"}}, wifiBox{}},
		{"wps enable", "/wifi-wps/config", url.Values{"enabled": {"1"}}, wifiBox{}},
		{"wps start", "/wifi-wps/start", url.Values{"bssid": {"00:11:22:33:44:55"}}, wifiBox{}},
		{"scan", "/wifi-radar/scan", url.Values{"ap": {"0"}}, wifiBox{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, c, writes := wifiSession(t, tc.box)
			if resp := post(t, c, srv, tc.path, tc.form); resp.StatusCode != http.StatusSeeOther {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if len(*writes) != 0 {
				t.Fatalf("unconfirmed request reached the box: %+v", *writes)
			}
		})
	}
}

func TestWifiRefusals(t *testing.T) {
	cases := []struct {
		name, path string
		form       url.Values
		box        wifiBox
	}{
		{"empty whitelist", "/wifi-filter/state", url.Values{"mac_filter_state": {"whitelist"}, "confirm": {"1"}}, wifiBox{filter: "disabled"}},
		{"bad mode", "/wifi-filter/state", url.Values{"mac_filter_state": {"all"}, "confirm": {"1"}}, wifiBox{}},
		{"bad mac", "/wifi-filter/entries", url.Values{"mac": {"nope"}, "type": {"whitelist"}}, wifiBox{}},
		{"short key", "/wifi/bss/00:11:22:33:44:55", url.Values{"ssid": {"X"}, "encryption": {"wpa2_psk_ccmp"}, "key": {"abc"}, "confirm": {"1"}}, wifiBox{}},
		{"bad encryption", "/wifi/bss/00:11:22:33:44:55", url.Values{"ssid": {"X"}, "encryption": {"none"}, "confirm": {"1"}}, wifiBox{}},
		{"bad bss id", "/wifi/bss/..%2Fx", url.Values{"ssid": {"X"}, "confirm": {"1"}}, wifiBox{}},
		{"bad width", "/wifi/ap/0", url.Values{"channel_width": {"7"}, "confirm": {"1"}}, wifiBox{}},
		{"bad ap id", "/wifi/ap/abc", url.Values{"channel_width": {"20"}, "confirm": {"1"}}, wifiBox{}},
		{"bad steering", "/wifi/steering", url.Values{"steering_level": {"9"}}, wifiBox{}},
		{"bad temp", "/wifi/temp-disable", url.Values{"minutes": {"0"}, "keep": {"5g"}, "confirm": {"1"}}, wifiBox{}},
		{"bad diag", "/wifi/diag/fix", url.Values{"target": {"bss:x"}, "code": {"all"}, "confirm": {"1"}}, wifiBox{}},
		{"guest bad access", "/wifi-guest/keys", url.Values{"access_type": {"root"}}, wifiBox{}},
		{"guest short key", "/wifi-guest/keys", url.Values{"access_type": {"net_only"}, "key": {"abc"}}, wifiBox{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, c, writes := wifiSession(t, tc.box)
			post(t, c, srv, tc.path, tc.form)
			if len(*writes) != 0 {
				t.Fatalf("invalid request reached the box: %+v", *writes)
			}
		})
	}
}

func TestWifiGuestGeneratesKey(t *testing.T) {
	srv, c, writes := wifiSession(t, wifiBox{})
	post(t, c, srv, "/wifi-guest/keys", url.Values{"access_type": {"net_only"}, "max_use_count": {"3"}, "duration": {"0"}})
	var body struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(lastWrite(t, writes).Body), &body); err != nil || len(body.Key) != 16 {
		t.Fatalf("generated key %q (%v)", body.Key, err)
	}
}

func TestWifiOpsDangerous(t *testing.T) {
	for _, name := range []string{"wifi_config_set", "wifi_ap_update", "wifi_ap_restart", "wifi_bss_update", "wifi_config_reset",
		"wifi_temp_disable_set", "wifi_planning_set", "wifi_mac_filter_delete", "wifi_wps_start", "wifi_guest_key_delete"} {
		op, ok := ops.Get(name)
		if !ok || !op.Dangerous {
			t.Errorf("%s: missing or not dangerous", name)
		}
	}
}
