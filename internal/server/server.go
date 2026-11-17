// Package server is the web interface.
package server

import (
	"bytes"
	"context"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
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
	Permissions(ctx context.Context) (map[string]bool, error)
}

var _ Box = (*fbx.Client)(nil)

// Server serves the web interface.
type Server struct {
	box      Box
	account  *account
	sessions *sessions
	limiter  *limiter
	pages    map[string]*template.Template
	log      *slog.Logger
	// setupCode must be typed on the first-run form: it is only printed in
	// the logs, so reaching the page is not enough to claim the box.
	setupCode string
}

// New loads the account from dataDir and parses the templates.
func New(box Box, dataDir string, log *slog.Logger) (*Server, error) {
	acct, err := loadAccount(dataDir)
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
	funcs := template.FuncMap{"version": func() string { return Version }}
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
	mux.Handle("GET /static/", http.FileServerFS(web.Static))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /setup", s.setupPage)
	mux.HandleFunc("POST /setup", s.setup)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.Handle("GET /{$}", s.guard(s.status))
	mux.Handle("GET /account", s.guard(s.accountPage))
	mux.Handle("POST /account", s.guard(s.changePassword))
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
		next(w, r)
	})
}

// render executes a page into a buffer first, so a template error becomes a
// clean 500 instead of half a page.
func (s *Server) render(w http.ResponseWriter, status int, page string, data map[string]any) {
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
