package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// callsBox serves anonymised telephony data and records the writes.
type callsBox struct {
	mu     sync.Mutex
	writes []string
}

func (b *callsBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "call/log/":
		body = `[{"id":7,"type":"missed","datetime":1700000000,"number":"0199000005","name":"Alice Test","duration":0,"new":true,"contact_id":3},
		         {"id":6,"type":"outgoing","datetime":1699990000,"number":"0639980000","name":"","duration":65,"new":false,"contact_id":0}]`
	case "call/account/":
		body = `{"phone_number":"0199000099"}`
	case "call/voicemail/":
		body = `[{"id":"20221215_154135_r1.au","phone_number":"0639980012","read":false,"duration":8,"country_code":33,"date":1671115295}]`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *callsBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	raw, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(raw))
	b.mu.Unlock()
	return nil
}

func (b *callsBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"calls": true}, nil
}

func (b *callsBox) lastWrites() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.writes...)
}

func TestCallsPage(t *testing.T) {
	srv := newTestServer(t, &callsBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/calls")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"Alice Test", "0199000099", "Manqué", "Émis", "0639980000", "20221215_154135_r1.au/read", "1 appel non lu"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestCallsWrites(t *testing.T) {
	box := &callsBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))

	post(t, c, srv, "/calls/7/read", nil)
	post(t, c, srv, "/calls/read-all", nil)
	confirm := url.Values{"confirm": {"1"}}
	post(t, c, srv, "/calls/6/delete", confirm)
	post(t, c, srv, "/calls/voicemail/20221215_154135_r1.au/read", nil)
	post(t, c, srv, "/calls/voicemail/20221215_154135_r1.au/delete", confirm)
	post(t, c, srv, "/calls/delete-all", confirm)

	want := []string{
		`PUT call/log/7/ {"new":false}`,
		`POST call/log/mark_all_as_read/ null`,
		`DELETE call/log/6/ null`,
		`PUT call/voicemail/20221215_154135_r1.au/ {"read":true}`,
		`DELETE call/voicemail/20221215_154135_r1.au/ null`,
		`POST call/log/delete_all/ null`,
	}
	got := box.lastWrites()
	if len(got) != len(want) {
		t.Fatalf("writes = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("write %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCallsDangerousNeedConfirm(t *testing.T) {
	box := &callsBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	post(t, c, srv, "/calls/delete-all", nil)
	post(t, c, srv, "/calls/6/delete", nil)
	post(t, c, srv, "/calls/voicemail/20221215_154135_r1.au/delete", nil)
	if w := box.lastWrites(); len(w) != 0 {
		t.Fatalf("unconfirmed dangerous actions wrote %q", w)
	}
}

func TestCallWho(t *testing.T) {
	c := callEntry{Name: "01 99 00 00 05", Number: "0199000005"}
	if c.ShowNumber() {
		t.Error("number repeated next to a name that is the same number")
	}
	if !(callEntry{Name: "Alice", Number: "0199000005"}).ShowNumber() {
		t.Error("number hidden next to a real name")
	}
	if (callEntry{Number: "0199000005"}).Who() != "0199000005" {
		t.Error("Who without name")
	}
}

func TestCallsLimit(t *testing.T) {
	n := callsLimit + 5
	srv := newTestServer(t, &bigCallsBox{n: n})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	_, body := get(t, c, srv, "/calls")
	if strings.Count(body, "/delete\"") != callsLimit+1 || !strings.Contains(body, "Tout afficher (105)") {
		t.Errorf("default page: want %d rows and the link", callsLimit)
	}
	_, body = get(t, c, srv, "/calls?all=1")
	if strings.Count(body, "/delete\"") != n+1 || strings.Contains(body, "Tout afficher") {
		t.Error("?all=1 must show everything")
	}
}

type bigCallsBox struct {
	callsBox
	n int
}

func (b *bigCallsBox) Get(ctx context.Context, path string, out any) error {
	if path != "call/log/" {
		return b.callsBox.Get(ctx, path, out)
	}
	var parts []string
	for i := range b.n {
		parts = append(parts, `{"id":`+strconv.Itoa(i)+`,"type":"missed","datetime":1700000000,"number":"0199000005","name":"","duration":0,"new":false}`)
	}
	return json.Unmarshal([]byte("["+strings.Join(parts, ",")+"]"), out)
}
