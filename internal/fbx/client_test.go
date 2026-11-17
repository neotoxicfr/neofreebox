package fbx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// fakeBox answers the login endpoints like a Freebox and serves one
// authenticated endpoint, "system/". expire makes the next authenticated
// call fail with auth_required, as when the box drops a session.
type fakeBox struct {
	token    string
	sessions atomic.Int32 // sessions opened
	expire   atomic.Bool
	current  atomic.Value // string: valid session token
}

func (f *fakeBox) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch r.URL.Path {
	case "/api/v16/login/":
		reply(200, map[string]any{"success": true, "result": map[string]any{"logged_in": false, "challenge": "challenge-1"}})
	case "/api/v16/login/session/":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["password"] != password(f.token, "challenge-1") || in["app_id"] != "test.app" {
			reply(403, map[string]any{"success": false, "error_code": "invalid_token", "msg": "Erreur d'authentification de l'application"})
			return
		}
		n := f.sessions.Add(1)
		tok := fmt.Sprintf("session-%d", n)
		f.current.Store(tok)
		reply(200, map[string]any{"success": true, "result": map[string]any{"session_token": tok, "permissions": map[string]bool{"settings": true}}})
	case "/api/v16/login/authorize/":
		reply(200, map[string]any{"success": true, "result": map[string]any{"app_token": "new-token", "track_id": 42}})
	case "/api/v16/login/authorize/42":
		reply(200, map[string]any{"success": true, "result": map[string]any{"status": "granted", "challenge": "x"}})
	case "/api/v16/system/":
		cur, _ := f.current.Load().(string)
		if f.expire.CompareAndSwap(true, false) || r.Header.Get("X-Fbx-App-Auth") != cur {
			reply(403, map[string]any{"success": false, "error_code": "auth_required", "msg": "Vous devez vous connecter"})
			return
		}
		reply(200, map[string]any{"success": true, "result": map[string]any{"model_info": map[string]string{"name": "fbxgw8-r1"}}})
	default:
		reply(404, map[string]any{"success": false, "error_code": "invalid_request", "msg": "Requête invalide"})
	}
}

func newTestClient(t *testing.T, f *fakeBox, token string) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, App{ID: "test.app", Name: "Test", Version: "1", Device: "ci"}, token)
	if err != nil {
		t.Fatal(err)
	}
	c.http = srv.Client() // the test server's self-signed certificate
	return c
}

type system struct {
	ModelInfo struct {
		Name string `json:"name"`
	} `json:"model_info"`
}

func TestSessionOpenedOnceAndReused(t *testing.T) {
	f := &fakeBox{token: "secret"}
	c := newTestClient(t, f, "secret")
	for range 3 {
		var s system
		if err := c.Get(context.Background(), "system/", &s); err != nil {
			t.Fatal(err)
		}
		if s.ModelInfo.Name != "fbxgw8-r1" {
			t.Fatalf("model = %q", s.ModelInfo.Name)
		}
	}
	if n := f.sessions.Load(); n != 1 {
		t.Fatalf("sessions opened = %d, want 1", n)
	}
	perms, err := c.Permissions(context.Background())
	if err != nil || !perms["settings"] {
		t.Fatalf("permissions = %v, %v", perms, err)
	}
}

func TestSessionReopenedOnAuthRequired(t *testing.T) {
	f := &fakeBox{token: "secret"}
	c := newTestClient(t, f, "secret")
	if err := c.Get(context.Background(), "system/", nil); err != nil {
		t.Fatal(err)
	}
	f.expire.Store(true)
	if err := c.Get(context.Background(), "system/", nil); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
	if n := f.sessions.Load(); n != 2 {
		t.Fatalf("sessions opened = %d, want 2", n)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name, token, path, code string
	}{
		{"wrong app token", "wrong", "system/", "invalid_token"},
		{"unknown endpoint", "secret", "nope/", "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, &fakeBox{token: "secret"}, tt.token)
			err := c.Get(context.Background(), tt.path, nil)
			if !IsCode(err, tt.code) {
				t.Fatalf("err = %v, want code %s", err, tt.code)
			}
		})
	}
	c := newTestClient(t, &fakeBox{token: "secret"}, "")
	if err := c.Get(context.Background(), "system/", nil); err == nil {
		t.Fatal("call without app token succeeded")
	}
}

func TestPairing(t *testing.T) {
	c := newTestClient(t, &fakeBox{}, "")
	tok, id, err := c.Authorize(context.Background())
	if err != nil || tok != "new-token" || id != 42 {
		t.Fatalf("Authorize = %q, %d, %v", tok, id, err)
	}
	st, err := c.AuthorizationStatus(context.Background(), id)
	if err != nil || st != StatusGranted {
		t.Fatalf("status = %q, %v", st, err)
	}
}

func TestEmbeddedCAs(t *testing.T) {
	if _, err := New("", App{}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestSafePath(t *testing.T) {
	for path, ok := range map[string]bool{
		"lan/browser/pub/":                         true,
		"lan/browser/pub/ether-00:11:22:33:44:55/": true,
		"fs/ls/L0Rpc3F1ZQ==?onlyFolder=1":          true,
		"lan/browser/../../system/reboot/":         false,
		"lan/browser/%2e%2e/system/":               false,
		"lan/browser/a%2Fb/":                       false,
		"lan/browser/./":                           false,
	} {
		if safePath(path) != ok {
			t.Errorf("safePath(%q) = %v, want %v", path, !ok, ok)
		}
	}
}
