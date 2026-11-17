package fbx

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // HMAC-SHA1 is what the Freebox login protocol requires
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// Authorization statuses, as returned while pairing.
const (
	StatusPending = "pending"
	StatusGranted = "granted"
	StatusDenied  = "denied"
	StatusTimeout = "timeout"
	StatusUnknown = "unknown"
)

// Authorize asks the box for an app token. The user must confirm the request
// on the box; follow it with AuthorizationStatus. Only works from the box's
// local network.
func (c *Client) Authorize(ctx context.Context) (token string, trackID int, err error) {
	in := map[string]string{
		"app_id":      c.app.ID,
		"app_name":    c.app.Name,
		"app_version": c.app.Version,
		"device_name": c.app.Device,
	}
	var out struct {
		AppToken string `json:"app_token"`
		TrackID  int    `json:"track_id"`
	}
	if err := c.call(ctx, http.MethodPost, "login/authorize/", "", in, &out); err != nil {
		return "", 0, err
	}
	if out.AppToken == "" {
		return "", 0, errors.New("fbx: the box returned no app token")
	}
	return out.AppToken, out.TrackID, nil
}

// AuthorizationStatus reports where a pairing request stands. The box only
// validates a token whose request was followed until it left "pending".
func (c *Client) AuthorizationStatus(ctx context.Context, trackID int) (string, error) {
	var out struct {
		Status string `json:"status"`
	}
	if err := c.call(ctx, http.MethodGet, "login/authorize/"+strconv.Itoa(trackID), "", nil, &out); err != nil {
		return "", err
	}
	return out.Status, nil
}

// Permissions returns the rights the box granted to the open session,
// opening it if needed.
func (c *Client) Permissions(ctx context.Context) (map[string]bool, error) {
	if _, err := c.ensureSession(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	perms := make(map[string]bool, len(c.perms))
	for k, v := range c.perms {
		perms[k] = v
	}
	return perms, nil
}

// Logout closes the session on the box.
func (c *Client) Logout(ctx context.Context) error {
	c.mu.Lock()
	sess := c.session
	c.session = ""
	c.mu.Unlock()
	if sess == "" {
		return nil
	}
	return c.call(ctx, http.MethodPost, "login/logout/", sess, nil, nil)
}

// ensureSession returns the current session token, opening a session if
// there is none. Concurrent callers wait for a single login.
func (c *Client) ensureSession(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != "" {
		return c.session, nil
	}
	if c.token == "" {
		return "", errors.New("fbx: no app token, pair the app first")
	}
	var ch struct {
		Challenge string `json:"challenge"`
	}
	if err := c.call(ctx, http.MethodGet, "login/", "", nil, &ch); err != nil {
		return "", fmt.Errorf("fbx: get challenge: %w", err)
	}
	in := map[string]string{
		"app_id":      c.app.ID,
		"app_version": c.app.Version,
		"password":    password(c.token, ch.Challenge),
	}
	var out struct {
		SessionToken string          `json:"session_token"`
		Permissions  map[string]bool `json:"permissions"`
	}
	if err := c.call(ctx, http.MethodPost, "login/session/", "", in, &out); err != nil {
		return "", fmt.Errorf("fbx: open session: %w", err)
	}
	if out.SessionToken == "" {
		return "", errors.New("fbx: open session: no session token")
	}
	c.session, c.perms = out.SessionToken, out.Permissions
	return c.session, nil
}

// password is hmac-sha1(app_token, challenge), hex-encoded.
func password(token, challenge string) string {
	m := hmac.New(sha1.New, []byte(token))
	m.Write([]byte(challenge))
	return hex.EncodeToString(m.Sum(nil))
}
