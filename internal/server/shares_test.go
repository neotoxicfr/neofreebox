package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// sharesBox serves anonymised share settings and records the writes.
type sharesBox struct {
	mu     sync.Mutex
	writes []string // "METHOD path body"
}

func (b *sharesBox) Get(_ context.Context, path string, out any) error {
	var body string
	switch path {
	case "netshare/samba/":
		body = `{"workgroup":"WORKGROUP","print_share_enabled":false,"file_share_enabled":true,"logon_enabled":true,"smbv2_enabled":true,"logon_user":"freebox"}`
	case "netshare/afp/":
		body = `{"enabled":false,"guest_allow":true,"login_name":"freebox","server_type":"airport"}`
	case "ftp/config/":
		body = `{"enabled":true,"allow_anonymous":false,"remote_domain":"203.0.113.7","username":"freebox","port_data":5411,"port_ctrl":17753,"weak_password":true,"allow_remote_access":false,"allow_anonymous_write":false}`
	case "tftp/config/":
		body = `{"enabled":false,"root":"L0ZyZWVib3gvdGZ0cA=="}`
	case "upnpav/config/":
		body = `{"enabled":true}`
	case "airmedia/config/":
		body = `{"enabled":false}`
	case "airmedia/receivers/":
		body = `[{"name":"Salon TV","password_protected":true,"capabilities":{"photo":true,"audio":true,"video":true,"screen":false}}]`
	default:
		return errors.New("unexpected path " + path)
	}
	return json.Unmarshal([]byte(body), out)
}

func (b *sharesBox) Do(ctx context.Context, method, path string, in, out any) error {
	if method == http.MethodGet {
		return b.Get(ctx, path, out)
	}
	j, _ := json.Marshal(in)
	b.mu.Lock()
	b.writes = append(b.writes, method+" "+path+" "+string(j))
	b.mu.Unlock()
	return nil
}

func (b *sharesBox) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true}, nil
}

func (b *sharesBox) last() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.writes) == 0 {
		return ""
	}
	return b.writes[len(b.writes)-1]
}

func (b *sharesBox) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.writes)
}

func TestSharesPage(t *testing.T) {
	srv := newTestServer(t, &sharesBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	resp, body := get(t, c, srv, "/shares")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"Partage Windows", "WORKGROUP", "Partage Mac", "airport", "203.0.113.7", "17753",
		`value="/Freebox/tftp"`, "UPnP AV", "Salon TV", "mot de passe faible", "SMBv2/v3 est actif"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "Le FTP est ouvert") {
		t.Error("FTP shown as exposed")
	}
}

func TestSharesWrites(t *testing.T) {
	box := &sharesBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))

	cases := []struct {
		path string
		form url.Values
		want string
	}{
		{"/shares/samba", url.Values{"file_share": {"1"}, "workgroup": {"HOME"}, "logon_user": {"bob"}, "logon_password": {"s3cret pass"}},
			`PUT netshare/samba/ {"file_share_enabled":true,"logon_enabled":false,"logon_password":"s3cret pass","logon_user":"bob","print_share_enabled":false,"workgroup":"HOME"}`},
		{"/shares/samba", url.Values{"file_share": {"1"}},
			`PUT netshare/samba/ {"file_share_enabled":true,"logon_enabled":false,"print_share_enabled":false}`},
		{"/shares/samba/smb", url.Values{"smbv2": {"1"}}, `PUT netshare/samba/ {"smbv2_enabled":true}`},
		{"/shares/samba/smb", url.Values{"confirm": {"1"}}, `PUT netshare/samba/ {"smbv2_enabled":false}`},
		{"/shares/afp", url.Values{"enabled": {"1"}, "server_type": {"macmini"}},
			`PUT netshare/afp/ {"enabled":true,"guest_allow":false,"server_type":"macmini"}`},
		{"/shares/ftp", url.Values{"enabled": {"1"}}, `PUT ftp/config/ {"enabled":true}`},
		{"/shares/ftp/exposure", url.Values{"confirm": {"1"}, "remote": {"1"}, "anonymous": {"1"}, "anonymous_write": {"1"}, "port_ctrl": {"2121"}},
			`PUT ftp/config/ {"allow_anonymous":true,"allow_anonymous_write":true,"allow_remote_access":true,"port_ctrl":2121}`},
		{"/shares/tftp", url.Values{"enabled": {"1"}, "root": {"/Freebox/tftp"}}, `PUT tftp/config/ {"enabled":true,"root":"L0ZyZWVib3gvdGZ0cA=="}`},
		{"/shares/upnpav", url.Values{"enabled": {"1"}}, `PUT upnpav/config/ {"enabled":true}`},
		{"/shares/airmedia", url.Values{"enabled": {"1"}, "password": {"3615"}}, `PUT airmedia/config/ {"enabled":true,"password":"3615"}`},
	}
	for _, tc := range cases {
		if resp := post(t, c, srv, tc.path, tc.form); resp.Header.Get("Location") != "/shares" {
			t.Fatalf("%s: redirect %q", tc.path, resp.Header.Get("Location"))
		}
		if got := box.last(); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.path, got, tc.want)
		}
	}
}

func TestSharesRefusals(t *testing.T) {
	box := &sharesBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))

	for _, tc := range []struct {
		path string
		form url.Values
	}{
		{"/shares/samba/smb", url.Values{}},                                            // SMBv1 without confirmation
		{"/shares/ftp/exposure", url.Values{"remote": {"1"}}},                          // exposure without confirmation
		{"/shares/ftp/exposure", url.Values{"confirm": {"1"}, "port_ctrl": {"70000"}}}, // bad port
		{"/shares/tftp", url.Values{"root": {"relative"}}},                             // not absolute
		{"/shares/afp", url.Values{"server_type": {"toaster"}}},                        // unknown type
	} {
		post(t, c, srv, tc.path, tc.form)
	}
	if n := box.count(); n != 0 {
		t.Fatalf("%d writes sent, want none: %v", n, box.writes)
	}
}
