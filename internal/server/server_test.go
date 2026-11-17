package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type fakeBox struct{ err error }

func (f fakeBox) Get(_ context.Context, path string, out any) error {
	if f.err != nil {
		return f.err
	}
	var body string
	switch path {
	case "system/":
		body = `{"firmware_version":"4.13.5","uptime_val":90061,"model_info":{"pretty_name":"Freebox v8 (r1)"},"sensors":[{"name":"Température CPU","value":51}]}`
	case "connection/":
		body = `{"state":"up","media":"ftth","ipv4":"192.0.2.10","rate_down":1250000,"rate_up":125000,"bandwidth_down":5000000000,"bandwidth_up":700000000}`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (f fakeBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true, "calls": true, "vm": false}, f.err
}

// setupCode is fixed in tests; the server prints a random one.
const setupCode = "testcode42"

func newTestServer(t *testing.T, box Box) *httptest.Server {
	t.Helper()
	s, err := New(box, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	s.setupCode = setupCode
	srv := httptest.NewTLSServer(s.Handler()) // TLS: the session cookie is Secure
	t.Cleanup(srv.Close)
	return srv
}

// browser keeps cookies and does not follow redirects, so tests see them.
func browser(srv *httptest.Server) *http.Client {
	c := srv.Client()
	c.Jar, _ = cookiejar.New(nil)
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

func post(t *testing.T, c *http.Client, srv *httptest.Server, path string, form url.Values) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func get(t *testing.T, c *http.Client, srv *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	resp, err := c.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func creds(user, pass string) url.Values {
	return url.Values{"username": {user}, "password": {pass}, "confirm": {pass}, "code": {setupCode}}
}

func TestFirstRunThenSignIn(t *testing.T) {
	srv := newTestServer(t, fakeBox{})
	c := browser(srv)

	if resp, _ := get(t, c, srv, "/"); resp.Header.Get("Location") != "/setup" {
		t.Fatalf("no account: / redirects to %q, want /setup", resp.Header.Get("Location"))
	}
	noCode := creds("admin", "correct horse battery")
	noCode.Set("code", "guess")
	if resp := post(t, c, srv, "/setup", noCode); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong setup code: status %d", resp.StatusCode)
	}
	if resp := post(t, c, srv, "/setup", creds("admin", "short")); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("short password: status %d", resp.StatusCode)
	}
	if resp := post(t, c, srv, "/setup", creds("admin", "correct horse battery")); resp.Header.Get("Location") != "/" {
		t.Fatalf("setup: redirect %q", resp.Header.Get("Location"))
	}
	resp, body := get(t, c, srv, "/")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Freebox v8 (r1)") || !strings.Contains(body, "10.0 Mb/s") {
		t.Fatalf("status page: %d\n%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "<li>settings</li>") || strings.Contains(body, "<li>vm</li>") {
		t.Fatal("rights list wrong")
	}

	// A second first run is refused, and the account is unchanged.
	other := browser(srv)
	post(t, other, srv, "/setup", creds("intruder", "another long password"))
	if resp, _ := get(t, other, srv, "/"); resp.Header.Get("Location") != "/login" {
		t.Fatal("second setup signed in")
	}

	post(t, c, srv, "/logout", nil)
	if resp, _ := get(t, c, srv, "/"); resp.Header.Get("Location") != "/login" {
		t.Fatal("still signed in after logout")
	}
	if resp := post(t, c, srv, "/login", creds("admin", "wrong password!")); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	if resp := post(t, c, srv, "/login", creds("admin", "correct horse battery")); resp.Header.Get("Location") != "/" {
		t.Fatal("sign-in failed")
	}
}

func TestSignInRateLimited(t *testing.T) {
	srv := newTestServer(t, fakeBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	for range 10 {
		post(t, c, srv, "/login", creds("admin", "wrong password!"))
	}
	if resp := post(t, c, srv, "/login", creds("admin", "correct horse battery")); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after 10 failures: %d, want 429", resp.StatusCode)
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	srv := newTestServer(t, fakeBox{})
	c := browser(srv)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/setup", strings.NewReader(creds("admin", "correct horse battery").Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site POST: %d, want 403", resp.StatusCode)
	}
}

func TestStatusWhenBoxDown(t *testing.T) {
	srv := newTestServer(t, fakeBox{err: errors.New("dial tcp: timeout")})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/")
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(body, "La box ne répond pas") {
		t.Fatalf("box down: %d", resp.StatusCode)
	}
}

func TestHeaders(t *testing.T) {
	srv := newTestServer(t, fakeBox{})
	resp, _ := get(t, browser(srv), srv, "/login")
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}
	if resp, body := get(t, browser(srv), srv, "/healthz"); resp.StatusCode != http.StatusOK || body != "ok\n" {
		t.Fatal("healthz")
	}
}
