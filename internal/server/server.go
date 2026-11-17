// Package server is the web interface.
package server

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/neotoxicfr/neofreebox/internal/fbx"
	"github.com/neotoxicfr/neofreebox/web"
)

// Version is set at build time; it stamps static asset URLs.
var Version = "dev"

const (
	cookieName = "__Host-neofreebox"
	sessionTTL = 7 * 24 * time.Hour
	maxForm    = 16 << 10
)

// Box is what the pages need from the box.
type Box interface {
	Get(ctx context.Context, path string, out any) error
	Do(ctx context.Context, method, path string, in, out any) error
	Permissions(ctx context.Context) (map[string]bool, error)
}

var _ Box = (*fbx.Client)(nil)

// Server serves the web interface.
type Server struct {
	box      Box
	account  *account
	authMu   sync.Mutex // serializes sign-in, token creation and password change
	tokens   *tokenStore
	sessions *sessions
	limiter  *limiter
	pages    map[string]*template.Template
	log      *slog.Logger
	// setupCode must be typed on the first-run form: it is only printed in
	// the logs, so reaching the page is not enough to claim the box.
	setupCode string
	// demo shows the "fictional data" banner (see EnableDemo).
	demo bool
}

// Demo account, created by EnableDemo and documented in the README.
const (
	DemoUser     = "demo"
	DemoPassword = "demo-neofreebox"
)

// EnableDemo marks the server as a demo: a banner says the data is fictional,
// and the demo account is created on first run so the app opens directly.
func (s *Server) EnableDemo() error {
	s.demo = true
	if s.account.exists() {
		return nil
	}
	s.setupCode = ""
	if err := s.account.set(DemoUser, DemoPassword, true); err != nil {
		return fmt.Errorf("create demo account: %w", err)
	}
	return nil
}

// New loads the account from dataDir and parses the templates.
func New(box Box, dataDir string, log *slog.Logger) (*Server, error) {
	acct, err := loadAccount(dataDir)
	if err != nil {
		return nil, err
	}
	tokens, err := loadTokens(dataDir)
	if err != nil {
		return nil, err
	}
	pages, err := parsePages()
	if err != nil {
		return nil, err
	}
	s := &Server{
		box:      box,
		account:  acct,
		tokens:   tokens,
		sessions: newSessions(sessionTTL),
		limiter:  &limiter{max: 10, window: 15 * time.Minute},
		pages:    pages,
		log:      log,
	}
	if !acct.exists() {
		s.setupCode = newSetupCode()
		log.Warn("no account yet: open the app and enter this setup code", "code", s.setupCode)
	}
	return s, nil
}

func parsePages() (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"version": func() string { return Version },
		"bits":    bits,
		"bytes":   byteSize,
		"since":   func(sec int64) string { return shortDuration(time.Duration(sec) * time.Second) },
		"band":    band,
		"date":    func(unix int64) string { return time.Unix(unix, 0).Format("02/01/2006 15:04") },
	}
	names, err := fs.Glob(web.Templates, "templates/*.html")
	if err != nil {
		return nil, err
	}
	pages := map[string]*template.Template{}
	for _, n := range names {
		if strings.HasSuffix(n, "/layout.html") {
			continue
		}
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(web.Templates, "templates/layout.html", n)
		if err != nil {
			return nil, err
		}
		pages[strings.TrimSuffix(strings.TrimPrefix(n, "templates/"), ".html")] = t
	}
	return pages, nil
}

// Handler returns the routes, wrapped in the security middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static := http.FileServerFS(web.Static)
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Asset URLs carry ?v=<version>: a new release changes the URL.
		if r.URL.Query().Has("v") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		static.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /setup", s.setupPage)
	mux.HandleFunc("POST /setup", s.setup)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.Handle("GET /account", s.guard(s.accountPage))
	mux.Handle("POST /account", s.guard(s.changePassword))
	mux.Handle("POST /account/tokens", s.guard(s.createToken))
	mux.Handle("POST /account/tokens/{id}/revoke", s.guard(s.revokeToken))
	for _, g := range menu() {
		for _, m := range g.Items {
			m.Routes(s, mux)
		}
	}
	mux.Handle("GET /api/v1/ops", s.bearer(http.HandlerFunc(s.apiList)))
	mux.Handle("POST /api/v1/ops/{name}", s.bearer(http.HandlerFunc(s.apiCall)))
	mux.Handle("/mcp", s.bearer(s.mcpHandler()))
	return headers(http.NewCrossOriginProtection().Handler(mux))
}

func headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// guard sends visitors without a session to sign-in, or to the first-run
// form while there is no account.
func (s *Server) guard(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.account.exists() {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		c, err := r.Cookie(cookieName)
		if err != nil || !s.sessions.valid(c.Value) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, maxForm) // no form uploads a file
		}
		next(w, r)
	})
}

// render executes a page into a buffer first, so a template error becomes a
// clean 500 instead of half a page.
func (s *Server) render(w http.ResponseWriter, status int, page string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["Menu"] = menu()
	data["Demo"] = s.demo
	var buf bytes.Buffer
	if err := s.pages[page].Execute(&buf, data); err != nil {
		s.log.Error("render", "page", page, "err", err)
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
