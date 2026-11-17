// Package fbx is a client for the Freebox OS API.
package fbx

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultURL is the box as seen from its local network. The certificate it
// serves is signed by one of Free's root CAs (ca.pem).
const DefaultURL = "https://mafreebox.freebox.fr"

// APIVersion is the major version of the API this client speaks.
const APIVersion = 16

//go:embed ca.pem
var caPEM []byte

// maxBody bounds a response body; the largest documented answers (file
// listings, programme guide) stay well under it.
const maxBody = 32 << 20

// App identifies this application to the box.
type App struct {
	ID      string // stable, never changes once a token was granted for it
	Name    string // shown on the box when the app asks for access
	Version string
	Device  string // the machine the app runs on
}

// Client talks to one box. It is safe for concurrent use; the session is
// opened on the first call and reopened whenever the box drops it.
type Client struct {
	base  string // e.g. https://mafreebox.freebox.fr/api/v16
	http  *http.Client
	app   App
	token string // app_token; empty until the app is paired

	mu      sync.Mutex
	session string
	perms   map[string]bool
}

// New returns a client for the box at baseURL (DefaultURL if empty). token is
// the app_token, or "" for a client that is only used to pair.
func New(baseURL string, app App, token string) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultURL
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("fbx: embedded CA certificates are invalid")
	}
	return &Client{
		base: strings.TrimRight(baseURL, "/") + fmt.Sprintf("/api/v%d", APIVersion),
		http: &http.Client{
			Timeout:   20 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		app:   app,
		token: token,
	}, nil
}

// Error is an API failure as reported by the box.
type Error struct {
	Status int    // HTTP status
	Code   string // error_code, e.g. "auth_required"
	Msg    string // msg, in French
}

func (e *Error) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("freebox: %s (%s)", e.Msg, e.Code)
	}
	return fmt.Sprintf("freebox: %s (HTTP %d)", e.Code, e.Status)
}

// IsCode reports whether err is an API error with the given error_code.
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

type envelope struct {
	Success   bool            `json:"success"`
	Result    json.RawMessage `json:"result"`
	ErrorCode string          `json:"error_code"`
	Msg       string          `json:"msg"`
}

// Get decodes the result of GET path into out (which may be nil).
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

// Do sends an authenticated request. path is relative to the API root
// ("lan/browser/pub/"). in, if not nil, is sent as JSON. A dropped session
// is reopened once.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	sess, err := c.ensureSession(ctx)
	if err != nil {
		return err
	}
	err = c.call(ctx, method, path, sess, in, out)
	if IsCode(err, "auth_required") {
		c.mu.Lock()
		if c.session == sess {
			c.session = ""
		}
		c.mu.Unlock()
		if sess, err = c.ensureSession(ctx); err != nil {
			return err
		}
		err = c.call(ctx, method, path, sess, in, out)
	}
	return err
}

// call sends one request; session may be empty for the unauthenticated
// login endpoints.
func (c *Client) call(ctx context.Context, method, path, session string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("fbx: encode %s: %w", path, err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/"+strings.TrimLeft(path, "/"), body)
	if err != nil {
		return fmt.Errorf("fbx: %s %s: %w", method, path, err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if session != "" {
		req.Header.Set("X-Fbx-App-Auth", session)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("fbx: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("fbx: read %s: %w", path, err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return &Error{Status: resp.StatusCode, Code: "bad_response", Msg: fmt.Sprintf("réponse illisible pour %s", path)}
	}
	if !env.Success {
		return &Error{Status: resp.StatusCode, Code: env.ErrorCode, Msg: env.Msg}
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("fbx: decode %s: %w", path, err)
		}
	}
	return nil
}
