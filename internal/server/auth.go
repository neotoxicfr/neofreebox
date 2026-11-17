package server

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

const minPassword = 12

func (s *Server) setupPage(w http.ResponseWriter, r *http.Request) {
	if s.account.exists() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "setup", nil)
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if s.account.exists() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	user, pass, ok := s.form(w, r)
	if !ok {
		return
	}
	if !s.limiter.allow() {
		s.render(w, http.StatusTooManyRequests, "setup", map[string]any{"Error": "Trop d'essais. Réessaie dans quelques minutes.", "Username": user})
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(r.PostFormValue("code"))), []byte(s.setupCode)) != 1 {
		s.limiter.fail()
		s.render(w, http.StatusUnauthorized, "setup", map[string]any{"Error": "Code de configuration incorrect : il est affiché dans les journaux du conteneur.", "Username": user})
		return
	}
	if msg := checkCredentials(user, pass, r.PostFormValue("confirm")); msg != "" {
		s.render(w, http.StatusBadRequest, "setup", map[string]any{"Error": msg, "Username": user})
		return
	}
	if err := s.account.set(user, pass, true); err != nil {
		if errors.Is(err, errAccountExists) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		s.log.Error("create account", "err", err)
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}
	s.startSession(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if !s.account.exists() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "login", nil)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := s.form(w, r)
	if !ok {
		return
	}
	if !s.limiter.allow() {
		s.render(w, http.StatusTooManyRequests, "login", map[string]any{"Error": "Trop d'essais. Réessaie dans quelques minutes.", "Username": user})
		return
	}
	if !s.account.check(user, pass) {
		s.limiter.fail()
		s.log.Warn("failed sign-in", "username", user)
		s.render(w, http.StatusUnauthorized, "login", map[string]any{"Error": "Identifiant ou mot de passe incorrect.", "Username": user})
		return
	}
	s.startSession(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.sessions.drop(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) accountPage(w http.ResponseWriter, _ *http.Request) {
	s.render(w, http.StatusOK, "account", map[string]any{"Nav": "account", "Username": s.account.Username})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxForm)
	cur, pass, confirm := r.PostFormValue("current"), r.PostFormValue("password"), r.PostFormValue("confirm")
	data := map[string]any{"Nav": "account", "Username": s.account.Username}
	if !s.limiter.allow() {
		data["Error"] = "Trop d'essais. Réessaie dans quelques minutes."
		s.render(w, http.StatusTooManyRequests, "account", data)
		return
	}
	if !s.account.check(s.account.Username, cur) {
		s.limiter.fail()
		data["Error"] = "Mot de passe actuel incorrect."
		s.render(w, http.StatusUnauthorized, "account", data)
		return
	}
	if msg := checkCredentials(s.account.Username, pass, confirm); msg != "" {
		data["Error"] = msg
		s.render(w, http.StatusBadRequest, "account", data)
		return
	}
	if err := s.account.set(s.account.Username, pass, false); err != nil {
		s.log.Error("change password", "err", err)
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}
	s.sessions.dropAll()
	s.startSession(w)
	data["Done"] = true
	s.render(w, http.StatusOK, "account", data)
}

// form reads username and password from a bounded POST body.
func (s *Server) form(w http.ResponseWriter, r *http.Request) (user, pass string, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Requête invalide", http.StatusBadRequest)
		return "", "", false
	}
	return strings.TrimSpace(r.PostFormValue("username")), r.PostFormValue("password"), true
}

// newSetupCode returns 10 random characters from an alphabet without
// look-alikes, easy to copy from a log line.
func newSetupCode() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func checkCredentials(user, pass, confirm string) string {
	switch {
	case user == "" || utf8.RuneCountInString(user) > 64:
		return "Choisis un identifiant (64 caractères au plus)."
	case utf8.RuneCountInString(pass) < minPassword:
		return "Le mot de passe doit faire au moins 12 caractères."
	case len(pass) > 72:
		return "Le mot de passe doit faire au plus 72 octets."
	case pass != confirm:
		return "Les deux mots de passe ne sont pas identiques."
	}
	return ""
}

func (s *Server) startSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: s.sessions.create(), Path: "/",
		MaxAge: int(sessionTTL.Seconds()), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}
