package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// account is the app's single user, stored as account.json in the data
// directory. It exists once the first-run form was filled in.
type account struct {
	path string

	mu       sync.Mutex
	Username string `json:"username"`
	Hash     []byte `json:"hash"` // bcrypt
}

func loadAccount(dir string) (*account, error) {
	a := &account{path: filepath.Join(dir, "account.json")}
	b, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read account: %w", err)
	}
	if err := json.Unmarshal(b, a); err != nil {
		return nil, fmt.Errorf("read account %s: %w", a.path, err)
	}
	return a, nil
}

func (a *account) exists() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Username != ""
}

// set writes a new username and password. create refuses to overwrite an
// existing account (first run); a password change passes create=false.
func (a *account) set(username, password string, create bool) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if create && a.Username != "" {
		return errAccountExists
	}
	b, err := json.Marshal(struct {
		Username string `json:"username"`
		Hash     []byte `json:"hash"`
	}{username, hash})
	if err != nil {
		return err
	}
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write account: %w", err)
	}
	if err := os.Rename(tmp, a.path); err != nil {
		return fmt.Errorf("write account: %w", err)
	}
	a.Username, a.Hash = username, hash
	return nil
}

// bcryptCost is lowered by the tests, which create an account each.
var bcryptCost = bcrypt.DefaultCost

var errAccountExists = errors.New("account already exists")

// check reports whether username and password match. The hash is compared
// even for a wrong username, so timing does not tell which was wrong.
func (a *account) check(username, password string) bool {
	a.mu.Lock()
	user, hash := a.Username, a.Hash
	a.mu.Unlock()
	if hash == nil {
		return false
	}
	ok := bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
	return ok && username == user
}

// sessions are kept in memory: a restart signs the user out.
type sessions struct {
	mu  sync.Mutex
	ids map[string]time.Time // id → expiry
	ttl time.Duration
}

func newSessions(ttl time.Duration) *sessions {
	return &sessions{ids: map[string]time.Time{}, ttl: ttl}
}

func (s *sessions) create() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	id := base64.RawURLEncoding.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, exp := range s.ids { // a handful at most: prune on the way
		if now.After(exp) {
			delete(s.ids, k)
		}
	}
	s.ids[id] = now.Add(s.ttl)
	return id
}

// valid reports whether id is a live session, and extends it.
func (s *sessions) valid(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.ids[id]
	if !ok || time.Now().After(exp) {
		delete(s.ids, id)
		return false
	}
	s.ids[id] = time.Now().Add(s.ttl)
	return true
}

func (s *sessions) drop(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ids, id)
}

// dropAll signs out every session, after a password change.
func (s *sessions) dropAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.ids)
}

// limiter allows max failed sign-ins per window, across the whole app: it
// sits behind a reverse proxy, so client IPs mean nothing, and it has a
// single account anyway.
type limiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	failures []time.Time
}

// allow reserves an attempt: concurrent requests cannot all pass before the
// first failure is counted. A successful attempt gives its place back with
// succeed.
func (l *limiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-l.window)
	i := 0
	for i < len(l.failures) && l.failures[i].Before(cut) {
		i++
	}
	l.failures = l.failures[i:]
	if len(l.failures) >= l.max {
		return false
	}
	l.failures = append(l.failures, time.Now())
	return true
}

// succeed releases the attempt reserved by the last allow.
func (l *limiter) succeed() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n := len(l.failures); n > 0 {
		l.failures = l.failures[:n-1]
	}
}
