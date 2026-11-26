package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/neotoxicfr/neofreebox/internal/fbx"
	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// apiToken is a key for the JSON API and the MCP server. Only its SHA-256 is
// stored; the token itself is shown once, when it is created.
type apiToken struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Hash    string    `json:"hash"`
	Created time.Time `json:"created"`
	// ReadOnly tokens may only call operations that change nothing.
	ReadOnly bool `json:"read_only,omitempty"`
}

type tokenStore struct {
	path    string
	mu      sync.Mutex
	list    []apiToken
	modTime time.Time // of tokens.json when last read or written
	// stale is set when a revocation could not be written: the file still
	// holds revoked tokens, so it is not read again until a write succeeds.
	stale bool
}

func loadTokens(dir string) (*tokenStore, error) {
	t := &tokenStore{path: filepath.Join(dir, "tokens.json")}
	if err := t.reload(true); err != nil {
		return nil, err
	}
	return t, nil
}

// reload reads tokens.json when it changed since the last read or write
// (or always, with force): a token created by `neofreebox token` while the
// server runs is seen without a restart. t.mu is held (or t is not shared).
func (t *tokenStore) reload(force bool) error {
	if t.stale {
		return nil
	}
	fi, err := os.Stat(t.path)
	if errors.Is(err, os.ErrNotExist) {
		t.list, t.modTime = nil, time.Time{} // a deleted file holds no token
		return nil
	}
	if err != nil {
		return fmt.Errorf("read tokens: %w", err)
	}
	if !force && !t.modTime.IsZero() && !fi.ModTime().After(t.modTime) {
		return nil
	}
	b, err := os.ReadFile(t.path)
	if err != nil {
		return fmt.Errorf("read tokens: %w", err)
	}
	var list []apiToken
	if err := json.Unmarshal(b, &list); err != nil {
		return fmt.Errorf("read tokens %s: %w", t.path, err)
	}
	t.list, t.modTime = list, fi.ModTime()
	return nil
}

// update runs one read-modify-write of tokens.json under an exclusive file
// lock shared with the `neofreebox token` command, so neither can write
// back a token the other has just revoked. t.mu is held.
func (t *tokenStore) update(change func([]apiToken) []apiToken) error {
	unlock, err := lockFile(t.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	if !t.stale {
		if err := t.reload(true); err != nil {
			return err
		}
	}
	return t.save(change(append([]apiToken(nil), t.list...)))
}

// CreateToken adds an API token to the data directory and returns it, for
// the `neofreebox token` command. A running server picks it up on its next
// API call.
func CreateToken(dataDir, name string, readOnly bool) (string, error) {
	t, err := loadTokens(dataDir)
	if err != nil {
		return "", err
	}
	return t.create(name, readOnly)
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// create returns a new token, to show once.
func (t *tokenStore) create(name string, readOnly bool) (string, error) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tok := "nfb_" + base64.RawURLEncoding.EncodeToString(b)
	id := make([]byte, 6)
	_, _ = rand.Read(id)
	t.mu.Lock()
	defer t.mu.Unlock()
	err := t.update(func(list []apiToken) []apiToken {
		return append(list, apiToken{
			ID: hex.EncodeToString(id), Name: name, Hash: hashToken(tok), Created: time.Now().UTC(), ReadOnly: readOnly,
		})
	})
	if err != nil {
		return "", err
	}
	return tok, nil
}

// revoke removes a token; it reports whether one had that id.
func (t *tokenStore) revoke(id string) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	found := false
	err := t.update(func(list []apiToken) []apiToken {
		kept := list[:0]
		for _, x := range list {
			if x.ID == id {
				found = true
				continue
			}
			kept = append(kept, x)
		}
		return kept
	})
	return found, err
}

// revokeAll removes every token, after a password change.
func (t *tokenStore) revokeAll() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.list = nil // invalid at once, even if the file cannot be rewritten
	if err := t.update(func([]apiToken) []apiToken { return nil }); err != nil {
		t.list, t.stale = nil, true
		return err
	}
	return nil
}

// save writes list and makes it current; t.mu is held.
func (t *tokenStore) save(list []apiToken) error {
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write tokens: %w", err)
	}
	if err := os.Rename(tmp, t.path); err != nil {
		return fmt.Errorf("write tokens: %w", err)
	}
	t.list, t.stale = list, false
	if fi, err := os.Stat(t.path); err == nil {
		t.modTime = fi.ModTime()
	}
	return nil
}

func (t *tokenStore) all() []apiToken {
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.reload(false)
	return append([]apiToken(nil), t.list...)
}

// lookup returns the stored token matching tok.
func (t *tokenStore) lookup(tok string) (apiToken, bool) {
	if !strings.HasPrefix(tok, "nfb_") {
		return apiToken{}, false
	}
	h := []byte(hashToken(tok))
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.reload(false) // on a read error the tokens already known stay valid
	var found apiToken
	ok := false
	for _, x := range t.list {
		if subtle.ConstantTimeCompare(h, []byte(x.Hash)) == 1 {
			found, ok = x, true
		}
	}
	return found, ok
}

type tokenKey struct{}

var errReadOnlyToken = errors.New("this API token is read-only: it can only call operations that change nothing and reveal no secret")

// bearer lets through requests carrying a valid API token.
func (s *Server) bearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		tok, valid := s.tokens.lookup(strings.TrimSpace(raw))
		if !ok || !valid {
			w.Header().Set("WWW-Authenticate", `Bearer realm="neofreebox"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid API token"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tokenKey{}, tok)))
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type opInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Dangerous   bool           `json:"dangerous,omitempty"`
	Arguments   map[string]any `json:"arguments"`
}

func (s *Server) apiList(w http.ResponseWriter, _ *http.Request) {
	list := []opInfo{}
	for _, op := range ops.All() {
		list = append(list, opInfo{op.Name, describe(op), op.Dangerous, op.Schema()})
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) apiCall(w http.ResponseWriter, r *http.Request) {
	op, ok := ops.Get(r.PathValue("name"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown operation"})
		return
	}
	args := map[string]any{}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err == nil && len(strings.TrimSpace(string(body))) > 0 {
		err = json.Unmarshal(body, &args)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body must be a JSON object of arguments"})
		return
	}
	res, err := s.callOp(r.Context(), op, args)
	if err != nil {
		writeJSON(w, statusOf(err), map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]json.RawMessage{"result": res})
}

func (s *Server) callOp(ctx context.Context, op ops.Op, args map[string]any) (json.RawMessage, error) {
	tok, _ := ctx.Value(tokenKey{}).(apiToken)
	if tok.ReadOnly && (!op.ReadOnly() || op.Secret) {
		return nil, errReadOnlyToken
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := ops.Call(ctx, s.box, op, args)
	if !op.ReadOnly() {
		s.log.Info("api operation", "op", op.Name, "token", tok.Name, "ok", err == nil)
	}
	return res, err
}

func statusOf(err error) int {
	if errors.Is(err, ops.ErrConfirm) {
		return http.StatusPreconditionRequired
	}
	if errors.Is(err, errReadOnlyToken) || fbx.IsCode(err, "read_only") || fbx.IsCode(err, "insufficient_rights") {
		return http.StatusForbidden
	}
	if strings.HasPrefix(err.Error(), "missing") || strings.HasPrefix(err.Error(), "argument") {
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

func describe(op ops.Op) string {
	d := fmt.Sprintf("%s (%s /api/v16/%s)", op.Description, op.Method, op.Path)
	if op.Body && op.BodyHelp != "" {
		d += " Body: " + op.BodyHelp
	}
	switch {
	case op.Dangerous:
		d += ` Dangerous: requires "confirm": true after the user agreed.`
	case op.Secret:
		d += ` Returns secrets (keys, passwords, share links): requires "confirm": true after the user agreed.`
	}
	return d
}

// mcpHandler serves every operation as an MCP tool, over streamable HTTP.
func (s *Server) mcpHandler() http.Handler {
	srv := mcp.NewServer(&mcp.Implementation{Name: "neofreebox", Version: Version}, &mcp.ServerOptions{
		Instructions: "Tools to read and change the settings of a Freebox (French ISP router) through the Freebox OS API. " +
			"Read-only tools are safe. Tools marked dangerous can cut the network or lose data: explain the effect to the user and only call them with confirm=true once they agreed.",
	})
	for _, op := range ops.All() {
		ro := op.ReadOnly() && !op.Secret // a client must not auto-approve reading a secret
		destructive := !ro                // the MCP default is true: say false only when nothing changes
		srv.AddTool(&mcp.Tool{
			Name:        op.Name,
			Description: describe(op),
			InputSchema: op.Schema(),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: ro, DestructiveHint: &destructive, OpenWorldHint: new(false)},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := map[string]any{}
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return errResult("arguments must be a JSON object"), nil //nolint:nilerr // a tool error is a result, not a protocol error
				}
			}
			res, err := s.callOp(ctx, op, args)
			if err != nil {
				return errResult(err.Error()), nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(res)}}}, nil
		})
	}
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}
