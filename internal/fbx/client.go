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
	"net"
	"net/http"
	"net/url"
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
	if !strings.HasPrefix(baseURL, "https://") {
		// The session token and the app token's proof travel in these requests.
		return nil, fmt.Errorf("fbx: the box URL must use https, not %q", baseURL)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("fbx: embedded CA certificates are invalid")
	}
	return &Client{
		base: strings.TrimRight(baseURL, "/") + fmt.Sprintf("/api/v%d", APIVersion),
		// No overall Timeout: it would also cut file downloads streamed by
		// Raw. Callers bound each call with their context; the transport
		// bounds connecting and waiting for headers.
		http: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig:       &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				MaxIdleConnsPerHost:   6, // pages read several endpoints at once
				IdleConnTimeout:       90 * time.Second,
			},
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
	if _, ok := ctx.Deadline(); !ok {
		// JSON answers are small: bound calls that come without a deadline
		// (the CLI), so a stalled body cannot hold the session lock forever.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Minute)
		defer cancel()
	}
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
	ctype := ""
	switch v := in.(type) {
	case nil:
	case url.Values: // the few endpoints that want a form (downloads/add)
		body, ctype = strings.NewReader(v.Encode()), "application/x-www-form-urlencoded"
	default:
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("fbx: encode %s: %w", Redact(path), err)
		}
		body, ctype = bytes.NewReader(b), "application/json"
	}
	resp, err := c.send(ctx, method, path, session, body, ctype)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("fbx: read %s: %w", Redact(path), err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return &Error{Status: resp.StatusCode, Code: "bad_response", Msg: fmt.Sprintf("réponse illisible pour %s", Redact(path))}
	}
	if !env.Success {
		return &Error{Status: resp.StatusCode, Code: env.ErrorCode, Msg: env.Msg}
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("fbx: decode %s: %w", Redact(path), err)
		}
	}
	return nil
}

// send performs one request and returns the response, whatever its status.
func (c *Client) send(ctx context.Context, method, path, session string, body io.Reader, ctype string) (*http.Response, error) {
	if !safePath(path) {
		return nil, fmt.Errorf("fbx: refusing path %q", Redact(path))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/"+strings.TrimLeft(path, "/"), body)
	if err != nil {
		return nil, fmt.Errorf("fbx: %s %s: %w", method, Redact(path), err)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if session != "" {
		req.Header.Set("X-Fbx-App-Auth", session)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err // the URL it carries holds the identifiers
		}
		return nil, fmt.Errorf("fbx: %s %s: %w", method, Redact(path), err)
	}
	return resp, nil
}

// Raw sends an authenticated GET for an endpoint that answers raw bytes
// (dl/ file downloads, voicemail audio) and returns the response for the
// caller to stream and close. A JSON answer is an error from the box.
func (c *Client) Raw(ctx context.Context, path string) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		sess, err := c.ensureSession(ctx)
		if err != nil {
			return nil, err
		}
		resp, err := c.send(ctx, http.MethodGet, path, sess, nil, "")
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusOK && !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
			return resp, nil
		}
		var env envelope
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&env)
		resp.Body.Close()
		if env.ErrorCode == "auth_required" && attempt == 0 {
			c.mu.Lock()
			if c.session == sess {
				c.session = ""
			}
			c.mu.Unlock()
			continue
		}
		code := env.ErrorCode
		if code == "" {
			code = "bad_response"
		}
		return nil, &Error{Status: resp.StatusCode, Code: code, Msg: env.Msg}
	}
}

// safePath rejects dot segments, so an argument spliced into a path can
// never reach another endpoint than the one the caller meant.
func safePath(path string) bool {
	p, _, _ := strings.Cut(path, "?")
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." || strings.Contains(seg, "\\") {
			return false
		}
		if u, err := url.PathUnescape(seg); err != nil || u == "." || u == ".." || strings.ContainsAny(u, "/\\") {
			return false
		}
	}
	return true
}

// Redact keeps the shape of an API path but hides its identifiers (host
// MACs, voicemail ids that carry phone numbers, base64 file paths): every
// segment that is not a plain lowercase word becomes {id}. Errors and logs
// use it, so they never carry personal data.
func Redact(path string) string {
	p, _, _ := strings.Cut(path, "?")
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		if strings.Trim(seg, "abcdefghijklmnopqrstuvwxyz_") != "" {
			segs[i] = "{id}"
		}
	}
	return strings.Join(segs, "/")
}
