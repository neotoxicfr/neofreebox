package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// dlWrite is a write the fake box received.
type dlWrite struct {
	Method, Path string
	Body         map[string]any
}

type downloadsBox struct {
	mu     sync.Mutex
	writes []dlWrite
}

const dlSchedule = `"normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal","normal"`

func dlFixtures() map[string]string {
	week := strings.Repeat(dlSchedule+",", 6) + dlSchedule
	return map[string]string{
		"downloads/": `[
			{"id":7,"type":"bt","name":"linux-image.iso","status":"downloading","size":1000000,"rx_pct":2500,"tx_pct":0,"rx_rate":125000,"tx_rate":1000,"rx_bytes":250000,"tx_bytes":1000,"eta":3600,"io_priority":"normal","error":"none","download_dir":"L0RhdGEv","created_ts":1700000000,"info_hash":"ABCDEF"},
			{"id":8,"type":"http","name":"fichier.zip","status":"stopped","size":500,"rx_pct":0,"io_priority":"low","error":"none","download_dir":"L0RhdGEv"}]`,
		"downloads/7":              `{"id":7,"type":"bt","name":"linux-image.iso","status":"downloading","size":1000000,"rx_pct":2500,"rx_rate":125000,"rx_bytes":250000,"tx_bytes":1000,"io_priority":"normal","error":"none","download_dir":"L0RhdGEv","info_hash":"ABCDEF"}`,
		"downloads/7/files":        `[{"id":"7-1","name":"disk.img","size":1000000,"rx":250000,"status":"queued","priority":"normal","error":"none"}]`,
		"downloads/7/log":          `"ligne un\nligne deux\n"`,
		"downloads/7/trackers":     `[{"announce":"udp://tracker.example.org:80","status":"announced","nseeders":12,"nleechers":3,"reannounce_in":600,"is_enabled":true}]`,
		"downloads/7/peers":        `[{"host":"192.0.2.9","port":6881,"state":"ready","origin":"tracker","client":"ClientBidon 1.0","country_code":"FR","progress":40,"rx":10,"tx":20}]`,
		"downloads/7/pieces":       `"XXX..--"`,
		"downloads/7/blacklist":    `[{"host":"192.0.2.66","reason":"timeout","expire":120,"global":true}]`,
		"downloads/stats":          `{"nb_tasks":2,"nb_tasks_downloading":1,"nb_tasks_stopped":1,"rx_rate":125000,"tx_rate":1000,"throttling_mode":"normal","throttling_is_scheduled":true,"conn_ready":true,"nb_peer":4,"nb_rss":1,"nb_rss_items_unread":3,"nzb_config_status":{"status":"not_checked","error":"none"}}`,
		"downloads/feeds/":         `[{"id":3,"title":"Flux bidon","url":"https://example.org/rss.xml","status":"ready","nb_read":1,"nb_unread":3,"auto_download":false,"fetch_ts":1700000000,"error":"none"}]`,
		"downloads/feeds/3":        `{"id":3,"title":"Flux bidon","url":"https://example.org/rss.xml","status":"ready"}`,
		"downloads/feeds/3/items/": `[{"id":31,"title":"Episode un","author":"auteur","is_read":false,"is_downloaded":false,"pub_ts":1700000000}]`,
		"downloads/config/": `{"max_downloading_tasks":5,"download_dir":"L0RhdGEv","watch_dir":"L3dhdGNoLw==","use_watch_dir":true,"dns1":"","dns2":"",
			"throttling":{"normal":{"rx_rate":0,"tx_rate":0},"slow":{"rx_rate":512000,"tx_rate":42000},"mode":"normal","schedule":[` + week + `]},
			"bt":{"max_peers":50,"stop_ratio":150,"crypto_support":"allowed","enable_dht":true,"enable_pex":true,"announce_timeout":30,"main_port":8860,"dht_port":40895},
			"news":{"server":"news.example.org","port":119,"user":"lecteur","nthreads":4,"auto_repair":true,"ssl":false},
			"feed":{"fetch_interval":60,"max_items":120}}`,
	}
}

func (b *downloadsBox) Get(_ context.Context, path string, out any) error {
	body, ok := dlFixtures()[path]
	if !ok {
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *downloadsBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	w := dlWrite{Method: method, Path: path}
	if form, ok := in.(url.Values); ok { // form bodies: flatten to compare like JSON ones
		flat := map[string]any{}
		for k := range form {
			flat[k] = form.Get(k)
		}
		in = flat
	}
	if in != nil {
		raw, _ := json.Marshal(in)
		_ = json.Unmarshal(raw, &w.Body)
	}
	b.mu.Lock()
	b.writes = append(b.writes, w)
	b.mu.Unlock()
	return nil
}

func (b *downloadsBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"downloader": true}, nil
}

func (b *downloadsBox) last(t *testing.T) dlWrite {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.writes) == 0 {
		t.Fatal("no write reached the box")
	}
	return b.writes[len(b.writes)-1]
}

func dlSetup(t *testing.T) (*downloadsBox, *httptest.Server, *http.Client) {
	t.Helper()
	box := &downloadsBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	return box, srv, c
}

func TestDownloadsPages(t *testing.T) {
	_, srv, c := dlSetup(t)
	for path, want := range map[string][]string{
		"/downloads": {"linux-image.iso", "fichier.zip", "25 %", "Flux bidon", "news.example.org", "lecteur", "512", "/downloads/7", "Supprimer les fichiers"},
		"/downloads/7": {"linux-image.iso", "disk.img", "udp://tracker.example.org:80", "192.0.2.9", "ClientBidon", "3 pièces terminées sur 7",
			"192.0.2.66", "ligne deux", "ABCDEF"},
		"/downloads/feeds/3": {"Episode un", "Flux bidon"},
	} {
		resp, body := get(t, c, srv, path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d\n%s", path, resp.StatusCode, body)
		}
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: missing %q", path, w)
			}
		}
	}
	if resp, _ := get(t, c, srv, "/downloads/abc"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("non numeric id: status %d", resp.StatusCode)
	}
}

func TestDownloadsNeverShowsPassword(t *testing.T) {
	_, srv, c := dlSetup(t)
	_, body := get(t, c, srv, "/downloads")
	if !strings.Contains(body, `type="password"`) || strings.Contains(strings.ToLower(body), `name="news_password" value`) {
		t.Fatal("password field must be empty")
	}
}

func TestDownloadsWrites(t *testing.T) {
	box, srv, c := dlSetup(t)
	for _, tc := range []struct {
		name, path string
		form       url.Values
		method     string
		wantPath   string
		wantBody   map[string]any
	}{
		{"pause", "/downloads/7/status", url.Values{"status": {"stopped"}}, "PUT", "downloads/7", map[string]any{"status": "stopped"}},
		{"priority", "/downloads/7/priority", url.Values{"io_priority": {"high"}}, "PUT", "downloads/7", map[string]any{"io_priority": "high"}},
		{"remove", "/downloads/7/delete", url.Values{"confirm": {"1"}}, "DELETE", "downloads/7", nil},
		{"erase", "/downloads/7/erase", url.Values{"confirm": {"1"}}, "DELETE", "downloads/7/erase", nil},
		{"file", "/downloads/7/file", url.Values{"file": {"7-1"}, "priority": {"no_dl"}}, "PUT", "downloads/7/files/7-1", map[string]any{"priority": "no_dl"}},
		{"tracker", "/downloads/7/tracker", url.Values{"announce": {"udp://tracker.example.org:80"}}, "POST", "downloads/7/trackers", map[string]any{"announce": "udp://tracker.example.org:80"}},
		{"block", "/downloads/7/block", url.Values{"host": {"192.0.2.66"}, "expire": {"3600"}}, "POST", "downloads/blacklist", map[string]any{"host": "192.0.2.66", "expire": float64(3600)}},
		{"unblock", "/downloads/7/unblock", url.Values{"host": {"192.0.2.66"}}, "DELETE", "downloads/blacklist/192.0.2.66", nil},
		{"clear", "/downloads/7/clear-blacklist", url.Values{}, "DELETE", "downloads/7/blacklist/empty", nil},
		{"add one", "/downloads/add", url.Values{"urls": {"magnet:?xt=urn:btih:abc"}, "dir": {"/Data/"}}, "POST", "downloads/add",
			map[string]any{"download_url": "magnet:?xt=urn:btih:abc", "download_dir": "L0RhdGEv"}},
		{"add many", "/downloads/add", url.Values{"urls": {"https://example.org/a\r\nftp://example.org/b"}}, "POST", "downloads/add",
			map[string]any{"download_url_list": "https://example.org/a\nftp://example.org/b"}},
		{"throttling", "/downloads/throttling", url.Values{"mode": {"slow"}}, "PUT", "downloads/throttling", map[string]any{"throttling": "slow"}},
		{"feed add", "/downloads/feeds/add", url.Values{"url": {"https://example.org/rss.xml"}}, "POST", "downloads/feeds/", map[string]any{"url": "https://example.org/rss.xml"}},
		{"feed auto", "/downloads/feeds/3/auto", url.Values{"auto": {"1"}}, "PUT", "downloads/feeds/3", map[string]any{"auto_download": true}},
		{"feed fetch", "/downloads/feeds/3/fetch", url.Values{}, "POST", "downloads/feeds/3/fetch", nil},
		{"feeds fetch all", "/downloads/feeds/fetch", url.Values{}, "POST", "downloads/feeds/fetch", nil},
		{"feed delete", "/downloads/feeds/3/delete", url.Values{"confirm": {"1"}}, "DELETE", "downloads/feeds/3", nil},
		{"feed read all", "/downloads/feeds/3/read", url.Values{}, "POST", "downloads/feeds/3/items/mark_all_as_read", nil},
		{"item download", "/downloads/feeds/3/items/31/download", url.Values{}, "POST", "downloads/feeds/3/items/31/download", nil},
		{"item read", "/downloads/feeds/3/items/31/read", url.Values{"read": {"1"}}, "PUT", "downloads/feeds/3/items/31", map[string]any{"is_read": true}},
	} {
		if resp := post(t, c, srv, tc.path, tc.form); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("%s: status %d", tc.name, resp.StatusCode)
		}
		got := box.last(t)
		if got.Method != tc.method || got.Path != tc.wantPath {
			t.Errorf("%s: sent %s %s, want %s %s", tc.name, got.Method, got.Path, tc.method, tc.wantPath)
		}
		for k, v := range tc.wantBody {
			if got.Body[k] != v {
				t.Errorf("%s: body %v, want %s=%v", tc.name, got.Body, k, v)
			}
		}
	}
}

func TestDownloadsDeletionsNeedConfirm(t *testing.T) {
	box, srv, c := dlSetup(t)
	for _, p := range []string{"/downloads/7/delete", "/downloads/7/erase", "/downloads/feeds/3/delete"} {
		post(t, c, srv, p, url.Values{})
	}
	if len(box.writes) != 0 {
		t.Fatalf("unconfirmed deletions reached the box: %v", box.writes)
	}
}

func TestDownloadsConfigAndSchedule(t *testing.T) {
	box, srv, c := dlSetup(t)
	form := url.Values{
		"download_dir": {"/Data/"}, "max_tasks": {"3"}, "use_watch_dir": {"1"}, "watch_dir": {"/watch/"},
		"normal_rx": {"0"}, "normal_tx": {"0"}, "slow_rx": {"500"}, "slow_tx": {"40"},
		"bt_max_peers": {"60"}, "bt_stop_ratio": {"200"}, "bt_crypto": {"preferred"}, "bt_port": {"8860"}, "bt_dht_port": {"40895"},
		"news_server": {"news.example.org"}, "news_port": {"563"}, "news_user": {"lecteur"}, "news_threads": {"2"},
		"feed_interval": {"30"}, "feed_max": {"100"},
	}
	post(t, c, srv, "/downloads/config", form)
	got := box.last(t)
	if got.Method != http.MethodPut || got.Path != "downloads/config/" || got.Body["max_downloading_tasks"] != float64(3) || got.Body["download_dir"] != "L0RhdGEv" {
		t.Fatalf("config write: %+v", got)
	}
	slow := got.Body["throttling"].(map[string]any)["slow"].(map[string]any)
	if slow["rx_rate"] != float64(500000) || len(got.Body["throttling"].(map[string]any)["schedule"].([]any)) != 168 {
		t.Errorf("throttling body: %v", got.Body["throttling"])
	}
	if _, has := got.Body["news"].(map[string]any)["password"]; has {
		t.Error("empty password field must not be sent")
	}

	form.Set("news_password", "s3cret")
	post(t, c, srv, "/downloads/config", form)
	if got := box.last(t); got.Body["news"].(map[string]any)["password"] != "s3cret" {
		t.Error("typed password not sent")
	}

	post(t, c, srv, "/downloads/schedule", url.Values{"day": {"1"}, "from": {"22"}, "to": {"24"}, "mode": {"hibernate"}})
	sched := box.last(t).Body["throttling"].(map[string]any)["schedule"].([]any)
	if sched[24+22] != "hibernate" || sched[24+23] != "hibernate" || sched[24+21] != "normal" || sched[22] != "normal" {
		t.Errorf("schedule slots wrong: %v", sched[20:50])
	}
}

func TestDownloadsDangerousAndInvalidSendNothing(t *testing.T) {
	box, srv, c := dlSetup(t)
	for path, form := range map[string]url.Values{
		"/downloads/7/erase":    {},
		"/downloads/add":        {"urls": {"file:///etc/passwd"}},
		"/downloads/7/status":   {"status": {"explode"}},
		"/downloads/7/priority": {"io_priority": {"urgent"}},
		"/downloads/7/tracker":  {"announce": {"nope"}},
		"/downloads/7/unblock":  {"host": {".."}},
		"/downloads/schedule":   {"day": {"9"}, "from": {"0"}, "to": {"1"}, "mode": {"slow"}},
		"/downloads/feeds/add":  {"url": {"javascript:alert(1)"}},
		"/downloads/throttling": {"mode": {"off"}},
		"/downloads/abc/delete": {},
		"/downloads/config":     {"bt_crypto": {"weird"}},
	} {
		post(t, c, srv, path, form)
	}
	if len(box.writes) != 0 {
		t.Fatalf("writes reached the box: %+v", box.writes)
	}
}
