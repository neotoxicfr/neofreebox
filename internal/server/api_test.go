package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

func init() {
	ops.Register(
		ops.Op{Name: "test_system_get", Description: "Box system info.", Path: "system/"},
		ops.Op{Name: "test_reboot", Description: "Reboot.", Method: http.MethodPost, Path: "system/reboot/", Dangerous: true},
	)
}

// newToken signs in, creates an API token on the account page and returns it.
func newToken(t *testing.T, srvURL string, c *http.Client, post func(path string, form url.Values) *http.Response) string {
	t.Helper()
	post("/setup", creds("admin", "correct horse battery"))
	req, _ := http.NewRequest(http.MethodPost, srvURL+"/account/tokens", strings.NewReader(url.Values{"name": {"ci"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	tok := regexp.MustCompile(`nfb_[A-Za-z0-9_-]+`).FindString(string(b))
	if tok == "" {
		t.Fatalf("no token on the page (%d)", resp.StatusCode)
	}
	return tok
}

func apiReq(t *testing.T, c *http.Client, method, u, tok, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, u, strings.NewReader(body))
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestAPIAndMCP(t *testing.T) {
	srv := newTestServer(t, fakeBox{})
	c := browser(srv)
	tok := newToken(t, srv.URL, c, func(p string, f url.Values) *http.Response { return post(t, c, srv, p, f) })
	api := srv.Client()

	if code, _ := apiReq(t, api, http.MethodGet, srv.URL+"/api/v1/ops", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	if code, _ := apiReq(t, api, http.MethodGet, srv.URL+"/api/v1/ops", "nfb_wrong", ""); code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", code)
	}
	code, body := apiReq(t, api, http.MethodGet, srv.URL+"/api/v1/ops", tok, "")
	if code != http.StatusOK || !strings.Contains(body, "test_system_get") {
		t.Fatalf("list: %d %s", code, body)
	}
	code, body = apiReq(t, api, http.MethodPost, srv.URL+"/api/v1/ops/test_system_get", tok, "")
	if code != http.StatusOK || !strings.Contains(body, "4.13.5") {
		t.Fatalf("call: %d %s", code, body)
	}
	if code, _ = apiReq(t, api, http.MethodPost, srv.URL+"/api/v1/ops/test_reboot", tok, `{}`); code != http.StatusPreconditionRequired {
		t.Fatalf("dangerous without confirm: %d", code)
	}
	if code, _ = apiReq(t, api, http.MethodPost, srv.URL+"/api/v1/ops/test_reboot", tok, `{"confirm":true}`); code != http.StatusOK {
		t.Fatalf("dangerous with confirm: %d", code)
	}

	// MCP, stateless JSON: list the tools, then call one.
	code, _ = apiReq(t, api, http.MethodPost, srv.URL+"/mcp", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("mcp without token: %d", code)
	}
	code, body = apiReq(t, api, http.MethodPost, srv.URL+"/mcp", tok, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != http.StatusOK || !strings.Contains(body, `"test_reboot"`) || !strings.Contains(body, `"destructiveHint":true`) {
		t.Fatalf("mcp tools/list: %d %s", code, body)
	}
	code, body = apiReq(t, api, http.MethodPost, srv.URL+"/mcp", tok, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"test_reboot","arguments":{}}}`)
	var rpc struct {
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(body), &rpc)
	if code != http.StatusOK || !rpc.Result.IsError || !strings.Contains(body, "confirm") {
		t.Fatalf("mcp dangerous call without confirm: %d %s", code, body)
	}
}
