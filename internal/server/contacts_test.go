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

// contactsBox serves anonymised contacts and records the writes.
type contactsBox struct {
	mu     sync.Mutex
	writes []string
}

func (b *contactsBox) Get(_ context.Context, path string, out any) error {
	if path != "contact/?start=0&limit=-1" {
		return errors.New("unexpected path " + path)
	}
	body := `[{"id":3,"display_name":"Alice Test","first_name":"Alice","last_name":"Test","company":"Exemple SA","notes":"",
	  "numbers":[{"id":11,"type":"mobile","number":"0639980011","is_default":false,"is_own":false}],
	  "emails":[{"id":12,"type":"home","email":"alice@example.org"}],
	  "addresses":[{"id":13,"type":"home","number":"11","street":"rue du Test","street2":"","zipcode":"75000","city":"Paris","country":"France"}],
	  "urls":[{"id":14,"type":"site","url":"https://example.org"}]},
	 {"id":4,"display_name":"Bob Exemple","first_name":"Bob","last_name":"Exemple","company":"","notes":""}]`
	return json.Unmarshal([]byte(body), out)
}

func (b *contactsBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	raw, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(raw))
	b.mu.Unlock()
	if out != nil && path == "contact/" {
		return json.Unmarshal([]byte(`{"id":9}`), out)
	}
	return nil
}

func (b *contactsBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"contacts": true}, nil
}

func (b *contactsBox) lastWrites() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.writes...)
}

func TestContactsPage(t *testing.T) {
	srv := newTestServer(t, &contactsBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/contacts")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"Alice Test", "0639980011", "alice@example.org", "11 rue du Test, 75000 Paris, France", "https://example.org", "Bob Exemple"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// Search by number ignores spaces; by name ignores case.
	if _, body := get(t, c, srv, "/contacts?q=06+11+22"); !strings.Contains(body, "Alice Test") || strings.Contains(body, "Bob Exemple") {
		t.Error("search by number wrong")
	}
	if _, body := get(t, c, srv, "/contacts?q=BOB"); strings.Contains(body, "Alice Test") || !strings.Contains(body, "Bob Exemple") {
		t.Error("search by name wrong")
	}
}

func TestContactsWrites(t *testing.T) {
	box := &contactsBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))

	post(t, c, srv, "/contacts/add", url.Values{"first_name": {"Carol"}, "last_name": {"Test"}, "type": {"mobile"}, "number": {"0639980001"}})
	post(t, c, srv, "/contacts/3/edit", url.Values{"first_name": {"Alice"}, "last_name": {"Test"}, "company": {"Autre"}})
	post(t, c, srv, "/contacts/3/numbers/add", url.Values{"type": {"work"}, "number": {"0199000000"}})
	post(t, c, srv, "/contacts/3/emails/add", url.Values{"type": {"home"}, "email": {"a@example.org"}})
	post(t, c, srv, "/contacts/3/numbers/11/delete", url.Values{"confirm": {"1"}})
	post(t, c, srv, "/contacts/3/delete", url.Values{"confirm": {"1"}})

	want := []string{
		`POST contact/ {"company":"","display_name":"Carol Test","first_name":"Carol","last_name":"Test","notes":""}`,
		`POST number/ {"contact_id":9,"number":"0639980001","type":"mobile"}`,
		`PUT contact/3/ {"company":"Autre","display_name":"Alice Test","first_name":"Alice","last_name":"Test","notes":""}`,
		`POST number/ {"contact_id":3,"number":"0199000000","type":"work"}`,
		`POST email/ {"contact_id":3,"email":"a@example.org","type":"home"}`,
		`DELETE number/11/ null`,
		`DELETE contact/3/ null`,
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

func TestContactsRefusals(t *testing.T) {
	box := &contactsBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	post(t, c, srv, "/contacts/3/delete", nil)                               // no confirmation
	post(t, c, srv, "/contacts/3/phones/add", url.Values{"number": {"1"}})   // unknown kind
	post(t, c, srv, "/contacts/x/numbers/add", url.Values{"number": {"1"}})  // bad id
	post(t, c, srv, "/contacts/3/numbers/add", url.Values{"type": {"work"}}) // empty field
	post(t, c, srv, "/contacts/add", url.Values{"number": {"0639980001"}})   // no name
	post(t, c, srv, "/contacts/3/edit", url.Values{"notes": {"only notes"}}) // no name
	post(t, c, srv, "/contacts/3/numbers/x/delete", nil)                     // bad related id
	post(t, c, srv, "/contacts/3/numbers/11/delete", nil)                    // no confirmation
	if w := box.lastWrites(); len(w) != 0 {
		t.Fatalf("refused requests wrote %q", w)
	}
}
