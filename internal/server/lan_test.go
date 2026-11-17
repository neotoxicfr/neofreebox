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

type lanBox struct {
	fakeBox
	writes []string // "METHOD path body"
}

func (b *lanBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "lan/config/":
		body = `{"local_domain":"home.example","name_dns":"freebox-server","name":"Freebox Server","name_mdns":"Freebox-Server","mode":"router","name_netbios":"Freebox_Server","ip":"192.168.1.254"}`
	case "lan/routes/":
		body = `[{"prefix":"192.168.42.0/24","gateway":"192.168.1.38","enabled":true,"description":"Lab"},{"prefix":"10.9.0.0/16","gateway":"192.168.1.39","enabled":false,"description":""}]`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *lanBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	raw, _ := json.Marshal(in)
	b.writes = append(b.writes, method+" "+path+" "+string(raw))
	return nil
}

func lanSetup(t *testing.T) (*lanBox, func(string, url.Values), func(string) string) {
	t.Helper()
	box := &lanBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	return box, func(p string, f url.Values) { post(t, c, srv, p, f) },
		func(p string) string {
			resp, body := get(t, c, srv, p)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: %d", p, resp.StatusCode)
			}
			return body
		}
}

func TestLanPage(t *testing.T) {
	_, _, page := lanSetup(t)
	body := page("/lan")
	for _, want := range []string{"Freebox Server", "192.168.1.254", "home.example", "192.168.42.0/24", "192.168.1.38", "Lab", "désactivé"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestLanWrites(t *testing.T) {
	box, send, _ := lanSetup(t)
	send("/lan/names", url.Values{"name": {"Box"}, "name_dns": {"box"}, "name_mdns": {"Box"}, "name_netbios": {"Box"}, "local_domain": {"home.example"}})
	send("/lan/network", url.Values{"mode": {"router"}, "ip": {"192.168.69.254"}, "confirm": {"1"}})
	send("/lan/routes/add", url.Values{"prefix": {"172.16.0.0/16"}, "gateway": {"192.168.1.40"}, "description": {"VPN"}, "confirm": {"1"}})
	send("/lan/routes/0/delete", url.Values{"confirm": {"1"}})
	want := []string{
		`PUT lan/config/ {"ip":"192.168.1.254","local_domain":"home.example","mode":"router","name":"Box","name_dns":"box","name_mdns":"Box","name_netbios":"Box"}`,
		`PUT lan/config/ {"ip":"192.168.69.254","local_domain":"home.example","mode":"router","name":"Freebox Server","name_dns":"freebox-server","name_mdns":"Freebox-Server","name_netbios":"Freebox_Server"}`,
		`PUT lan/routes/ [{"prefix":"192.168.42.0/24","gateway":"192.168.1.38","enabled":true,"description":"Lab"},{"prefix":"10.9.0.0/16","gateway":"192.168.1.39","enabled":false,"description":""},{"prefix":"172.16.0.0/16","gateway":"192.168.1.40","enabled":true,"description":"VPN"}]`,
		`PUT lan/routes/ [{"prefix":"10.9.0.0/16","gateway":"192.168.1.39","enabled":false,"description":""}]`,
	}
	if strings.Join(box.writes, "\n") != strings.Join(want, "\n") {
		t.Fatalf("writes:\n%s\nwant:\n%s", strings.Join(box.writes, "\n"), strings.Join(want, "\n"))
	}
}

func TestLanDangerousNeedsConfirm(t *testing.T) {
	box, send, _ := lanSetup(t)
	send("/lan/network", url.Values{"mode": {"bridge"}, "ip": {"192.168.1.254"}})
	send("/lan/routes/add", url.Values{"prefix": {"172.16.0.0/16"}, "gateway": {"192.168.1.40"}})
	send("/lan/routes/0/delete", nil)
	send("/lan/network", url.Values{"mode": {"router"}, "ip": {"not an ip"}, "confirm": {"1"}})
	send("/lan/routes/9/delete", url.Values{"confirm": {"1"}})
	if len(box.writes) != 0 {
		t.Fatalf("unexpected writes: %v", box.writes)
	}
}
