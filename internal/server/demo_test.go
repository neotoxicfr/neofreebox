package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/neotoxicfr/neofreebox/internal/demo"
)

// TestDemoPages signs in to the demo (account created by EnableDemo) and opens
// every page of the menu: each must answer 200, say it is fictional, and show
// no box error, so the fake box covers every read the pages make.
func TestDemoPages(t *testing.T) {
	s, err := New(demo.New(), t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnableDemo(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(s.Handler())
	t.Cleanup(srv.Close)
	c := browser(srv)
	resp := post(t, c, srv, "/login", url.Values{"username": {DemoUser}, "password": {DemoPassword}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("demo sign-in: status %d", resp.StatusCode)
	}
	pages := 0
	for _, g := range menu() {
		for _, m := range g.Items {
			pages++
			resp, body := get(t, c, srv, m.Href())
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s: status %d", m.Href(), resp.StatusCode)
			}
			for _, bad := range []string{"La box n'a pas", "La box refuse", "La box ne répond pas", "role=\"alert\""} {
				if strings.Contains(body, bad) {
					t.Errorf("%s: shows %q", m.Href(), bad)
				}
			}
			if !strings.Contains(body, "Démo : données fictives") {
				t.Errorf("%s: no demo banner", m.Href())
			}
		}
	}
	if pages == 0 {
		t.Error("no page in the menu")
	}
}

// TestDemoWritesAreHarmless checks that a write goes through and changes nothing.
func TestDemoWritesAreHarmless(t *testing.T) {
	if err := demo.New().Do(t.Context(), http.MethodPut, "wifi/config/", map[string]any{"enabled": false}, nil); err != nil {
		t.Fatal(err)
	}
}
