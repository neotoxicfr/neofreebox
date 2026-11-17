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

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// apiToken is a key for the JSON API and the MCP server. Only its SHA-256 is
// stored; the token itself is shown once, when it is created.
type apiToken struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Hash    string    `json:"hash"`
	Created time.Time `json:"created"`
}

type tokenStore struct {
	path string
	mu   sync.Mutex
	list []apiToken
}

func loadTokens(dir string) (*tokenStore, error) {
	t := &tokenStore{path: filepath.Join(dir, "tokens.json")}
	b, err := os.ReadFile(t.path)
	if errors.Is(err, os.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read tokens: %w", err)
	}
	if err := json.Unmarshal(b, &t.list); err != nil {
		return nil, fmt.Errorf("read tokens %s: %w", t.path, err)
	}
	return t, nil
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// create returns a new token, to show once.
func (t *tokenStore) create(name string) (string, error) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tok := "nfb_" + base64.RawURLEncoding.EncodeToString(b)
	id := make([]byte, 6)
	_, _ = rand.Read(id)
	t.mu.Lock()
	defer t.mu.Unlock()
	list := append(append([]apiToken(nil), t.list...), apiToken{
		ID: hex.EncodeToString(id), Name: name, Hash: hashToken(tok), Created: time.Now().UTC(),
	})
	if err := t.save(list); err != nil {
		return "", err
	}
	return tok, nil
}

func (t *tokenStore) revoke(id string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var list []apiToken
	for _, x := range t.list {
		if x.ID != id {
			list = append(list, x)
		}
	}
	return t.save(list)
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
	t.list = list
	return nil
}

func (t *tokenStore) all() []apiToken {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]apiToken(nil), t.list...)
}

func (t *tokenStore) valid(tok string) bool {
	if !strings.HasPrefix(tok, "nfb_") {
		return false
	}
	h := []byte(hashToken(tok))
	t.mu.Lock()
	defer t.mu.Unlock()
	ok := false
	for _, x := range t.list {
		if subtle.ConstantTimeCompare(h, []byte(x.Hash)) == 1 {
			ok = true
		}
	}
	return ok
}

// bearer lets through requests carrying a valid API token.
func (s *Server) bearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !s.tokens.valid(strings.TrimSpace(tok)) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="neofreebox"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid API token"})
			return
		}
		next.ServeHTTP(w, r)
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := ops.Call(ctx, s.box, op, args)
	if op.Method != http.MethodGet {
		s.log.Info("api operation", "op", op.Name, "ok", err == nil)
	}
	return res, err
}

func statusOf(err error) int {
	if errors.Is(err, ops.ErrConfirm) {
		return http.StatusPreconditionRequired
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
	if op.Dangerous {
		d += ` Dangerous: requires "confirm": true after the user agreed.`
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
		ro := op.Method == http.MethodGet
		destructive := op.Dangerous
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
