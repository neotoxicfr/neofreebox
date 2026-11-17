package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Shares: the file and media services of the box (Windows/Mac sharing, FTP,
// TFTP, UPnP AV, AirMedia). Passwords are write-only: an empty field leaves
// them unchanged and they are never rendered.

func init() {
	register(module{Slug: "shares", Title: "Partages", Group: "Contenus", Order: 20, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /shares", s.shares)
		s.page(mux, "POST /shares/samba", s.saveSamba)
		s.page(mux, "POST /shares/samba/smb", s.saveSambaProtocol)
		s.page(mux, "POST /shares/afp", s.saveAfp)
		s.page(mux, "POST /shares/ftp", s.saveFtp)
		s.page(mux, "POST /shares/ftp/exposure", s.saveFtpExposure)
		s.page(mux, "POST /shares/tftp", s.saveTftp)
		s.page(mux, "POST /shares/upnpav", s.saveUpnpav)
		s.page(mux, "POST /shares/airmedia", s.saveAirmedia)
	}})
	ops.Register(
		ops.Op{Name: "netshare_samba_get", Description: "Get the Windows file sharing (Samba) settings: file_share_enabled, print_share_enabled, logon_enabled, logon_user, workgroup, smbv2_enabled.",
			Path: "netshare/samba/"},
		ops.Op{Name: "netshare_samba_update", Description: "Update the Samba settings. Setting smbv2_enabled to false falls back to the insecure SMBv1; logon_password is write-only.", Method: http.MethodPut,
			Path: "netshare/samba/", Body: true, Dangerous: true,
			BodyHelp: `{"file_share_enabled": true, "print_share_enabled": false, "logon_enabled": true, "logon_user": "freebox", "logon_password": "a long password", "workgroup": "WORKGROUP", "smbv2_enabled": true}`},
		ops.Op{Name: "netshare_afp_get", Description: "Get the Mac file sharing (AFP) settings: enabled, guest_allow, login_name, server_type (icon shown on macOS).",
			Path: "netshare/afp/"},
		ops.Op{Name: "netshare_afp_update", Dangerous: true, Description: "Update the AFP settings; login_password is write-only. server_type is one of powerbook, powermac, macmini, imac, macbook, macbookpro, macbookair, macpro, appletv, airport, xserve.", Method: http.MethodPut,
			Path: "netshare/afp/", Body: true,
			BodyHelp: `{"enabled": true, "guest_allow": false, "login_name": "freebox", "login_password": "a long password", "server_type": "macmini"}`},
		ops.Op{Name: "ftp_config_get", Description: "Get the FTP server settings: enabled, allow_anonymous, allow_anonymous_write, username, allow_remote_access, weak_password, port_ctrl, port_data, remote_domain.",
			Path: "ftp/config/"},
		ops.Op{Name: "ftp_config_update", Description: "Update the FTP server settings. Remote or anonymous access exposes the files; remote access needs a strong password (password is write-only).", Method: http.MethodPut,
			Path: "ftp/config/", Body: true, Dangerous: true,
			BodyHelp: `{"enabled": true, "allow_anonymous": false, "allow_anonymous_write": false, "allow_remote_access": false, "password": "a long password", "port_ctrl": 21, "port_data": 20}`},
		ops.Op{Name: "tftp_config_get", Description: "Get the TFTP server settings: enabled and root (base64 of the absolute directory path on the storage).",
			Path: "tftp/config/"},
		ops.Op{Name: "tftp_config_update", Dangerous: true, Description: "Update the TFTP server settings; root is the base64 of an absolute path.", Method: http.MethodPut,
			Path: "tftp/config/", Body: true, BodyHelp: `{"enabled": true, "root": "L1NlcnZldXIvdGZ0cA=="}`},
		ops.Op{Name: "upnpav_config_get", Description: "Get the UPnP AV (media server) settings: enabled.",
			Path: "upnpav/config/"},
		ops.Op{Name: "upnpav_config_update", Description: "Enable or disable the UPnP AV media server.", Method: http.MethodPut,
			Path: "upnpav/config/", Body: true, BodyHelp: `{"enabled": true}`},
		ops.Op{Name: "airmedia_config_get", Description: "Get the AirMedia receiver settings of the box: enabled (the password is write-only).",
			Path: "airmedia/config/"},
		ops.Op{Name: "airmedia_config_update", Description: "Enable or disable the AirMedia receiver; a non-empty password makes clients enter it.", Method: http.MethodPut,
			Path: "airmedia/config/", Body: true, BodyHelp: `{"enabled": true, "password": "3615"}`},
		ops.Op{Name: "airmedia_receivers_list", Description: "List the AirMedia receivers reachable by the box: name, password_protected, capabilities (photo, audio, video, screen).",
			Path: "airmedia/receivers/"},
		ops.Op{Name: "airmedia_receiver_send", Description: "Start or stop a photo or video on an AirMedia receiver. For video, media is a URL; for a photo, a base64 file path on the box storage. position is percent*1000.", Method: http.MethodPost, Dangerous: true,
			Path: "airmedia/receivers/{name}/", Params: []ops.Param{{Name: "name", Description: `Receiver name, e.g. "Freebox Player"`}},
			Body: true, BodyHelp: `{"action": "start", "media_type": "video", "media": "http://example.com/clip.mp4", "password": "", "position": 0}`},
	)
}

type sambaConfig struct {
	FileShare  bool   `json:"file_share_enabled"`
	PrintShare bool   `json:"print_share_enabled"`
	Logon      bool   `json:"logon_enabled"`
	LogonUser  string `json:"logon_user"`
	Workgroup  string `json:"workgroup"`
	SMBv2      bool   `json:"smbv2_enabled"`
}

type afpConfig struct {
	Enabled    bool   `json:"enabled"`
	GuestAllow bool   `json:"guest_allow"`
	LoginName  string `json:"login_name"`
	ServerType string `json:"server_type"`
}

type ftpConfig struct {
	Enabled        bool   `json:"enabled"`
	Anonymous      bool   `json:"allow_anonymous"`
	AnonymousWrite bool   `json:"allow_anonymous_write"`
	Username       string `json:"username"`
	Remote         bool   `json:"allow_remote_access"`
	WeakPassword   bool   `json:"weak_password"`
	PortCtrl       int    `json:"port_ctrl"`
	PortData       int    `json:"port_data"`
	RemoteDomain   string `json:"remote_domain"`
}

// Exposed says whether the FTP server can be reached from the internet or
// without a password.
func (f ftpConfig) Exposed() bool { return f.Remote || f.Anonymous }

type tftpConfig struct {
	Enabled bool   `json:"enabled"`
	Root    string `json:"root"` // base64 of an absolute path
	Path    string `json:"-"`    // Root, decoded
}

type sharesEnabled struct {
	Enabled bool `json:"enabled"`
}

type airmediaReceiver struct {
	Name              string          `json:"name"`
	PasswordProtected bool            `json:"password_protected"`
	Capabilities      map[string]bool `json:"capabilities"`
}

// Caps lists the receiver's capabilities in French, in a fixed order.
func (a airmediaReceiver) Caps() []string {
	var out []string
	for _, c := range []struct{ key, label string }{{"photo", "photo"}, {"audio", "audio"}, {"video", "vidéo"}, {"screen", "écran"}} {
		if a.Capabilities[c.key] {
			out = append(out, c.label)
		}
	}
	return out
}

var afpServerTypes = []string{"powerbook", "powermac", "macmini", "imac", "macbook", "macbookpro", "macbookair", "macpro", "appletv", "airport", "xserve"}

func (s *Server) shares(w http.ResponseWriter, r *http.Request) {
	var (
		samba     sambaConfig
		afp       afpConfig
		ftp       ftpConfig
		tftp      tftpConfig
		upnp      sharesEnabled
		air       sharesEnabled
		receivers []airmediaReceiver
	)
	var err error
	for _, f := range []struct {
		path string
		out  any
	}{{"netshare/samba/", &samba}, {"netshare/afp/", &afp}, {"ftp/config/", &ftp}, {"tftp/config/", &tftp},
		{"upnpav/config/", &upnp}, {"airmedia/config/", &air}, {"airmedia/receivers/", &receivers}} {
		if err = s.fetch(r.Context(), f.path, f.out); err != nil {
			err = fmt.Errorf("%s: %w", f.path, err)
			break
		}
	}
	if b, derr := base64.StdEncoding.DecodeString(tftp.Root); derr == nil {
		tftp.Path = string(b)
	}
	s.show(w, r, "shares", map[string]any{"Samba": samba, "Afp": afp, "Ftp": ftp, "Tftp": tftp, "Upnp": upnp, "Air": air,
		"Receivers": receivers, "AfpTypes": afpServerTypes}, err)
}

// sharesSecret adds a write-only password to body only when one was typed.
func sharesSecret(r *http.Request, body map[string]any, name string) {
	if v := r.PostFormValue(name); v != "" {
		body[name] = v
	}
}

// sharesRefuse flashes an error and goes back to the page.
func sharesRefuse(w http.ResponseWriter, r *http.Request, msg string) {
	setFlash(w, "err:"+msg)
	http.Redirect(w, r, "/shares", http.StatusSeeOther)
}

func (s *Server) saveSamba(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{
		"file_share_enabled":  formBool(r, "file_share"),
		"print_share_enabled": formBool(r, "print_share"),
		"logon_enabled":       formBool(r, "logon"),
	}
	if v := formStr(r, "workgroup"); v != "" {
		body["workgroup"] = v
	}
	if v := formStr(r, "logon_user"); v != "" {
		body["logon_user"] = v
	}
	sharesSecret(r, body, "logon_password")
	// Sharing files without a password opens them to every device on the LAN.
	if formBool(r, "file_share") && !formBool(r, "logon") && !confirmed(w, r, "/shares") {
		return
	}
	s.change(w, r, http.MethodPut, "netshare/samba/", body, "Partage Windows enregistré.", "/shares")
}

// saveSambaProtocol switches SMBv2/v3 on or off. Off means the old,
// insecure SMBv1, so it needs the confirmation.
func (s *Server) saveSambaProtocol(w http.ResponseWriter, r *http.Request) {
	v2 := formBool(r, "smbv2")
	if !v2 && !confirmed(w, r, "/shares") {
		return
	}
	done := "SMBv2/v3 réactivé."
	if !v2 {
		done = "SMBv1 activé : à éviter."
	}
	s.change(w, r, http.MethodPut, "netshare/samba/", map[string]any{"smbv2_enabled": v2}, done, "/shares")
}

func (s *Server) saveAfp(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{"enabled": formBool(r, "enabled"), "guest_allow": formBool(r, "guest_allow")}
	if formBool(r, "enabled") && formBool(r, "guest_allow") && !confirmed(w, r, "/shares") {
		return
	}
	if v := formStr(r, "login_name"); v != "" {
		body["login_name"] = v
	}
	if v := formStr(r, "server_type"); v != "" {
		if !slices.Contains(afpServerTypes, v) {
			sharesRefuse(w, r, "Type de serveur inconnu.")
			return
		}
		body["server_type"] = v
	}
	sharesSecret(r, body, "login_password")
	s.change(w, r, http.MethodPut, "netshare/afp/", body, "Partage Mac enregistré.", "/shares")
}

// saveFtp changes what is not exposure: on/off and the password. Enabling
// the server publishes it with the exposure already set, so it is confirmed.
func (s *Server) saveFtp(w http.ResponseWriter, r *http.Request) {
	if formBool(r, "enabled") && !confirmed(w, r, "/shares") {
		return
	}
	body := map[string]any{"enabled": formBool(r, "enabled")}
	sharesSecret(r, body, "password")
	s.change(w, r, http.MethodPut, "ftp/config/", body, "FTP enregistré.", "/shares")
}

// saveFtpExposure sets who can reach the FTP server: always confirmed,
// since it can open the files to the internet or to anyone.
func (s *Server) saveFtpExposure(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/shares") {
		return
	}
	anon := formBool(r, "anonymous")
	body := map[string]any{
		"allow_anonymous":       anon,
		"allow_anonymous_write": anon && formBool(r, "anonymous_write"),
		"allow_remote_access":   formBool(r, "remote"),
	}
	for _, name := range []string{"port_ctrl", "port_data"} {
		if n := formInt(r, name); n != 0 {
			if n < 1 || n > 65535 {
				sharesRefuse(w, r, "Port invalide (1 à 65535).")
				return
			}
			body[name] = n
		}
	}
	s.change(w, r, http.MethodPut, "ftp/config/", body, "Accès FTP enregistré.", "/shares")
}

func (s *Server) saveTftp(w http.ResponseWriter, r *http.Request) {
	root := formStr(r, "root")
	if !strings.HasPrefix(root, "/") || path.Clean(root) != root || root == "/" {
		sharesRefuse(w, r, "Le dossier racine doit être un sous-dossier absolu, comme /Freebox/tftp.")
		return
	}
	// TFTP has no authentication: anything under root is readable on the LAN.
	if formBool(r, "enabled") && !confirmed(w, r, "/shares") {
		return
	}
	s.change(w, r, http.MethodPut, "tftp/config/",
		map[string]any{"enabled": formBool(r, "enabled"), "root": base64.StdEncoding.EncodeToString([]byte(root))},
		"TFTP enregistré.", "/shares")
}

func (s *Server) saveUpnpav(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodPut, "upnpav/config/", map[string]any{"enabled": formBool(r, "enabled")}, "UPnP AV enregistré.", "/shares")
}

func (s *Server) saveAirmedia(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{"enabled": formBool(r, "enabled")}
	sharesSecret(r, body, "password")
	s.change(w, r, http.MethodPut, "airmedia/config/", body, "AirMedia enregistré.", "/shares")
}
