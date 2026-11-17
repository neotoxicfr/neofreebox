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

// profilesBox serves anonymised parental-control data and records writes.
type profilesBox struct {
	mu     sync.Mutex
	writes []string
}

func (b *profilesBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "profile/":
		body = `[{"id":3,"name":"Enfants","url":"/resources/images/profile/profile_02.png"}]`
	case "profile/3/":
		body = `{"id":3,"name":"Enfants","url":"/resources/images/profile/profile_02.png"}`
	case "parental/config/":
		body = `{"default_filter_mode":"allowed"}`
	case "lan/browser/pub/":
		body = `[{"id":"ether-00:11:22:33:44:55","primary_name":"Tablette","active":true,"l2ident":{"id":"00:11:22:33:44:55"}},
			{"id":"ether-00:11:22:33:44:66","primary_name":"Console","active":true,"l2ident":{"id":"00:11:22:33:44:66"}}]`
	case "network_control/3/":
		body = `{"profile_id":3,"next_change":1790000000,"override":false,"override_mode":"denied","override_until":0,
			"current_mode":"allowed","rule_mode":"allowed","macs":["00:11:22:33:44:55"],"hosts":[{"primary_name":"Tablette"}],"resolution":288,"cdayranges":[]}`
	case "network_control/3/rules/":
		body = `[{"id":1,"profile_id":3,"name":"Nuit","mode":"denied","start_time":79200,"end_time":25200,"weekdays":[true,true,true,true,true,false,false,false],"enabled":true}]`
	case "network_control/3/rules/1/":
		body = `{"id":1,"profile_id":3,"name":"Nuit","mode":"denied","start_time":79200,"end_time":25200,"weekdays":[true,true,true,true,true,false,false,false],"enabled":false}`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *profilesBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	j, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(j))
	b.mu.Unlock()
	return nil
}

func (b *profilesBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true}, nil
}

func (b *profilesBox) last() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.writes) == 0 {
		return ""
	}
	return b.writes[len(b.writes)-1]
}

func (b *profilesBox) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.writes)
}

func profilesSetup(t *testing.T) (*profilesBox, *http.Client, string, func(path string, f url.Values) *http.Response) {
	t.Helper()
	box := &profilesBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	return box, c, srv.URL, func(path string, f url.Values) *http.Response { return post(t, c, srv, path, f) }
}

func TestProfilesPage(t *testing.T) {
	srv := newTestServer(t, &profilesBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/profiles")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"Enfants", "Nuit", "Lun, Mar, Mer, Jeu, Ven", "22:00 – 07:00", "Tablette", "00:11:22:33:44:66", "Autorisé"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestProfilesPageBoxDown(t *testing.T) {
	srv := newTestServer(t, fakeBox{err: errors.New("timeout")})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	if resp, _ := get(t, c, srv, "/profiles"); resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", resp.StatusCode)
	}
}

func TestProfilesWrites(t *testing.T) {
	box, _, _, send := profilesSetup(t)
	tests := []struct {
		name, path string
		form       url.Values
		want       string
	}{
		{"add", "/profiles/add", url.Values{"name": {"Ados"}}, `POST profile/ {"name":"Ados","url":"/resources/images/profile/profile_04.png"}`},
		{"rename", "/profiles/3/rename", url.Values{"name": {"Kids"}}, `PUT profile/3/ {"name":"Kids","url":"/resources/images/profile/profile_02.png"}`},
		{"delete", "/profiles/3/delete", url.Values{"confirm": {"1"}}, `DELETE profile/3/ null`},
		{"attach", "/profiles/3/attach", url.Values{"mac": {"00:11:22:33:44:66"}}, `"macs":["00:11:22:33:44:55","00:11:22:33:44:66"]`},
		{"detach", "/profiles/3/detach", url.Values{"mac": {"00:11:22:33:44:55"}}, `"macs":[]`},
		{"resume", "/profiles/3/resume", nil, `"override":false`},
		{"pause", "/profiles/3/pause", url.Values{"confirm": {"1"}, "minutes": {"0"}}, `"override":true`},
		{"rule add", "/profiles/3/rules/add", url.Values{"confirm": {"1"}, "mode": {"denied"}, "start": {"21:30"}, "end": {"00:00"}, "day5": {"1"}, "name": {"Soir"}},
			`POST network_control/3/rules/ {"enabled":true,"end_time":86400,"mode":"denied","name":"Soir","start_time":77400,"weekdays":[false,false,false,false,false,true,false,false]}`},
		{"rule enable", "/profiles/3/rules/1/toggle", url.Values{"confirm": {"1"}, "enable": {"1"}}, `"enabled":true`},
		{"rule delete", "/profiles/3/rules/1/delete", nil, `DELETE network_control/3/rules/1/ null`},
		{"default", "/profiles/default", url.Values{"confirm": {"1"}, "mode": {"denied"}}, `PUT parental/config/ {"default_filter_mode":"denied"}`},
	}
	for _, tc := range tests {
		before := box.count()
		send(tc.path, tc.form)
		if box.count() != before+1 || !strings.Contains(box.last(), tc.want) {
			t.Errorf("%s: wrote %q, want it to contain %q", tc.name, box.last(), tc.want)
		}
	}
	if strings.Contains(box.last(), "hosts") {
		t.Error("read-only hosts sent back")
	}
}

func TestProfilesDangerousNeedConfirm(t *testing.T) {
	box, _, _, send := profilesSetup(t)
	for _, tc := range []struct {
		path string
		form url.Values
	}{
		{"/profiles/3/delete", nil},
		{"/profiles/3/pause", url.Values{"minutes": {"60"}}},
		{"/profiles/default", url.Values{"mode": {"denied"}}},
		{"/profiles/3/rules/add", url.Values{"mode": {"denied"}, "start": {"22:00"}, "end": {"07:00"}, "day0": {"1"}}},
		{"/profiles/3/rules/1/toggle", url.Values{"enable": {"1"}}},
	} {
		if resp := send(tc.path, tc.form); resp.StatusCode != http.StatusSeeOther {
			t.Errorf("%s: status %d", tc.path, resp.StatusCode)
		}
	}
	if n := box.count(); n != 0 {
		t.Fatalf("%d writes without confirmation: %s", n, box.last())
	}
}

func TestProfilesBadInput(t *testing.T) {
	box, _, _, send := profilesSetup(t)
	send("/profiles/add", url.Values{"name": {" "}})
	send("/profiles/3/rules/add", url.Values{"mode": {"denied"}, "confirm": {"1"}, "start": {"22:07"}, "end": {"07:00"}, "day0": {"1"}})
	send("/profiles/3/rules/add", url.Values{"mode": {"denied"}, "confirm": {"1"}, "start": {"22:00"}, "end": {"07:00"}})
	send("/profiles/default", url.Values{"mode": {"bogus"}, "confirm": {"1"}})
	send("/profiles/x/delete", url.Values{"confirm": {"1"}})
	if n := box.count(); n != 0 {
		t.Fatalf("%d writes for invalid input: %s", n, box.last())
	}
}
