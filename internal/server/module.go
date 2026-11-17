package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A module is one section of the app (devices, DHCP, Wi-Fi…). Each lives in
// its own file and registers itself from init(): its menu entry, its pages
// and, in internal/ops, its operations for the API and MCP server.
type module struct {
	Slug  string // URL prefix and template name: "dhcp" → /dhcp, templates/dhcp.html
	Title string // menu label
	Group string // menu section, one of groupOrder
	Order int    // position within the group
	// Routes adds the module's handlers; wrap each with s.guard (see s.page).
	Routes func(s *Server, mux *http.ServeMux)
}

// Href is the module's menu link.
func (m module) Href() string {
	if m.Slug == "status" {
		return "/"
	}
	return "/" + m.Slug
}

// groupOrder is the order of the menu sections.
var groupOrder = []string{"Freebox", "Réseau", "Wi-Fi", "VPN", "Supervision", "Système", "Contenus", "Téléphonie", "Maison"}

var (
	modulesMu sync.Mutex
	modules   []module
)

func register(m module) {
	modulesMu.Lock()
	defer modulesMu.Unlock()
	modules = append(modules, m)
}

// navGroup is a menu section, as the layout renders it.
type navGroup struct {
	Name  string
	Items []module
}

func menu() []navGroup {
	modulesMu.Lock()
	defer modulesMu.Unlock()
	var groups []navGroup
	for _, g := range groupOrder {
		var items []module
		for _, m := range modules {
			if m.Group == g {
				items = append(items, m)
			}
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].Order < items[j].Order })
		if len(items) > 0 {
			groups = append(groups, navGroup{g, items})
		}
	}
	return groups
}

// page registers a guarded handler.
func (s *Server) page(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	mux.Handle(pattern, s.guard(h))
}

// fetch reads one endpoint of the box, with a timeout.
func (s *Server) fetch(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.box.Get(ctx, path, out)
}

// show renders a module page. err, if not nil, is shown instead of the data
// (the box did not answer); data["Nav"] is set to the module's slug.
func (s *Server) show(w http.ResponseWriter, r *http.Request, slug string, data map[string]any, err error) {
	if data == nil {
		data = map[string]any{}
	}
	data["Nav"] = slug
	status := http.StatusOK
	if err != nil {
		s.log.Error("page", "page", slug, "err", err)
		data["Error"] = "La box a refusé ou n'a pas répondu : " + err.Error()
		status = http.StatusBadGateway
	}
	data["Flash"] = takeFlash(w, r)
	s.render(w, status, slug, data)
}

// change sends a write to the box, then redirects to back with a message.
// It is what every form handler ends with.
func (s *Server) change(w http.ResponseWriter, r *http.Request, method, path string, body any, done, back string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.box.Do(ctx, method, path, body, nil); err != nil {
		s.log.Error("change", "method", method, "path", path, "err", err)
		setFlash(w, "err:"+err.Error())
	} else {
		s.log.Info("change", "method", method, "path", path)
		setFlash(w, "ok:"+done)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// confirmed reports whether a dangerous form was confirmed (a required
// "confirm" checkbox). If not, it redirects back with an error.
//
//nolint:unused // used by the modules being added
func confirmed(w http.ResponseWriter, r *http.Request, back string) bool {
	if r.PostFormValue("confirm") == "1" {
		return true
	}
	setFlash(w, "err:Coche la case de confirmation pour valider cette action.")
	http.Redirect(w, r, back, http.StatusSeeOther)
	return false
}

const flashCookie = "neofreebox-flash"

func setFlash(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: base64.RawURLEncoding.EncodeToString([]byte(msg)),
		Path: "/", MaxAge: 60, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// flash is a one-time message after a form: Ok or not, and its text.
type flash struct {
	Ok   bool
	Text string
}

func takeFlash(w http.ResponseWriter, r *http.Request) *flash {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	b, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}
	kind, text, _ := strings.Cut(string(b), ":")
	return &flash{Ok: kind == "ok", Text: text}
}

// Form helpers: a missing or malformed value gives the zero value.

//nolint:unused // used by the modules being added
func formBool(r *http.Request, name string) bool {
	v := r.PostFormValue(name)
	return v == "1" || v == "on" || v == "true"
}

//nolint:unused // used by the modules being added
func formInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue(name)))
	return n
}

func formStr(r *http.Request, name string) string {
	return strings.TrimSpace(r.PostFormValue(name))
}
