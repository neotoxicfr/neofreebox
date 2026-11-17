package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// VPN client: the box connects to a remote VPN; one configuration is active
// at a time and, once it is, all the LAN traffic goes through the tunnel.

func init() {
	register(module{Slug: "vpn-client", Title: "Client VPN", Group: "VPN", Order: 10, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /vpn-client", s.vpnClient)
		s.page(mux, "POST /vpn-client/add", s.vpnClientAdd)
		s.page(mux, "POST /vpn-client/{id}/rename", s.vpnClientRename)
		s.page(mux, "POST /vpn-client/{id}/activate", s.vpnClientActivate)
		s.page(mux, "POST /vpn-client/{id}/deactivate", s.vpnClientDeactivate)
		s.page(mux, "POST /vpn-client/{id}/delete", s.vpnClientDelete)
	}})
	ops.Register(
		ops.Op{Name: "vpn_client_configs_list", Description: "List the VPN client configurations: id, description, type (pptp/openvpn/wireguard), active, conf_pptp / conf_wireguard (remote host, keys). Contains secrets.",
			Path: "vpn_client/config/"},
		ops.Op{Name: "vpn_client_config_get", Description: "Get one VPN client configuration.",
			Path: "vpn_client/config/{id}", Params: []ops.Param{{Name: "id", Description: `Configuration id, e.g. "vpn0"`}}},
		ops.Op{Name: "vpn_client_config_add", Description: "Add a VPN client configuration. With active=true the box connects at once and routes all its traffic through the tunnel.", Method: http.MethodPost,
			Path: "vpn_client/config/", Body: true, Dangerous: true,
			BodyHelp: `{"type": "wireguard", "description": "office", "active": false, "conf_wireguard": {"remote_addr": "203.0.113.5", "remote_port": 51820, "remote_public_key": "<base64>", "local_priv_key": "<base64>", "local_addr": [{"ip": "10.0.0.2", "len": 32}], "dns": ["10.0.0.1"], "mtu": 1420}} or {"type": "pptp", "description": "x", "active": false, "conf_pptp": {"remote_host": "vpn.example.org", "username": "u", "password": "p", "mppe": "require", "allowed_auth": {"mschapv2": true}}}`},
		ops.Op{Name: "vpn_client_config_update", Description: "Update a VPN client configuration (description, parameters, or active: true/false). Activating it routes all the box's traffic through the VPN; only one configuration is active at a time.", Method: http.MethodPut,
			Path: "vpn_client/config/{id}", Params: []ops.Param{{Name: "id", Description: `Configuration id, e.g. "vpn0"`}},
			Body: true, BodyHelp: `{"active": true}`, Dangerous: true},
		ops.Op{Name: "vpn_client_config_delete", Description: "Delete a VPN client configuration.", Method: http.MethodDelete,
			Path: "vpn_client/config/{id}", Params: []ops.Param{{Name: "id", Description: `Configuration id, e.g. "vpn0"`}}, Dangerous: true},
		ops.Op{Name: "vpn_client_status_get", Description: "Get the VPN client status: enabled, active_vpn, state (down/waiting_wan/going_up/up/going_down), last_error, stats (rates and bytes), ipv4 (assigned address, gateway, dns).",
			Path: "vpn_client/status"},
		ops.Op{Name: "vpn_client_log_get", Description: "Get the VPN client log, as text.",
			Path: "vpn_client/log"},
	)
}

type vpnClientConfig struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Active      bool   `json:"active"`
	PPTP        struct {
		RemoteHost string `json:"remote_host"`
		Username   string `json:"username"`
		MPPE       string `json:"mppe"`
	} `json:"conf_pptp"`
	WG struct {
		RemoteAddr string `json:"remote_addr"`
		RemotePort int    `json:"remote_port"`
	} `json:"conf_wireguard"` // keys are deliberately not read: they never reach a page
}

// Remote is where the tunnel goes.
func (c vpnClientConfig) Remote() string {
	switch c.Type {
	case "pptp":
		return c.PPTP.RemoteHost
	case "wireguard":
		if c.WG.RemotePort > 0 {
			return c.WG.RemoteAddr + ":" + strconv.Itoa(c.WG.RemotePort)
		}
		return c.WG.RemoteAddr
	}
	return ""
}

type vpnClientStatus struct {
	Enabled     bool   `json:"enabled"`
	ActiveVPN   string `json:"active_vpn"`
	Description string `json:"active_vpn_description"`
	Type        string `json:"type"`
	State       string `json:"state"`
	LastError   string `json:"last_error"`
	LastUp      int64  `json:"last_up"`
	LastTry     int64  `json:"last_try"`
	NextTry     int64  `json:"next_try"`
	Stats       struct {
		RateUp    int64 `json:"rate_up"`
		RateDown  int64 `json:"rate_down"`
		BytesUp   int64 `json:"bytes_up"`
		BytesDown int64 `json:"bytes_down"`
	} `json:"stats"`
	IPv4 struct {
		IPMask struct {
			IP   string `json:"ip"`
			Mask string `json:"mask"`
		} `json:"ip_mask"`
		Gateway string   `json:"gateway"`
		DNS     []string `json:"dns"`
	} `json:"ipv4"`
}

// StateLabel is the connection state in French.
func (s vpnClientStatus) StateLabel() string {
	switch s.State {
	case "up":
		return "connecté"
	case "down":
		return "déconnecté"
	case "going_up":
		return "connexion en cours"
	case "going_down":
		return "déconnexion en cours"
	case "waiting_wan":
		return "en attente d'internet"
	}
	return s.State
}

// ErrorLabel explains the last error in French; empty when there is none.
func (s vpnClientStatus) ErrorLabel() string {
	switch s.LastError {
	case "", "none":
		return ""
	case "authentication_failed", "auth_failed":
		return "identifiants refusés"
	case "resolv_failed":
		return "nom d'hôte introuvable"
	case "connect_timeout":
		return "délai dépassé"
	case "connect_failed":
		return "connexion impossible"
	case "setup_control_failed", "setup_call_failed", "protocol":
		return "erreur de protocole"
	case "remote_terminated", "remote_disconnect":
		return "fermée par le serveur distant"
	}
	return s.LastError
}

func (s *Server) vpnClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var cfgs []vpnClientConfig
	var st vpnClientStatus
	var logText string
	err := s.fetch(ctx, "vpn_client/config/", &cfgs)
	if e := s.fetch(ctx, "vpn_client/status", &st); err == nil {
		err = e
	}
	// The log is a nicety: a failure here must not hide the rest.
	_ = s.fetch(ctx, "vpn_client/log", &logText)
	s.show(w, r, "vpn-client", map[string]any{"Configs": cfgs, "Status": st, "Log": logText}, err)
}

func vpnClientBad(w http.ResponseWriter, r *http.Request, msg string) {
	setFlash(w, "err:"+msg)
	http.Redirect(w, r, "/vpn-client", http.StatusSeeOther)
}

func (s *Server) vpnClientAdd(w http.ResponseWriter, r *http.Request) {
	desc, typ := formStr(r, "description"), formStr(r, "type")
	if desc == "" {
		vpnClientBad(w, r, "Donne un nom à la configuration.")
		return
	}
	body := map[string]any{"type": typ, "description": desc, "active": false}
	switch typ {
	case "pptp":
		host, user := formStr(r, "remote_host"), formStr(r, "username")
		if host == "" || user == "" {
			vpnClientBad(w, r, "Le serveur et l'identifiant sont obligatoires.")
			return
		}
		body["conf_pptp"] = map[string]any{"remote_host": host, "username": user, "password": r.PostFormValue("password"),
			"mppe": formStr(r, "mppe"), "allowed_auth": map[string]bool{"mschapv2": true}}
	case "wireguard":
		ip, length := formStr(r, "local_ip"), formInt(r, "local_len")
		addr, port, pub, priv := formStr(r, "remote_addr"), formInt(r, "remote_port"), formStr(r, "remote_public_key"), formStr(r, "local_priv_key")
		if addr == "" || port < 1 || port > 65535 || pub == "" || priv == "" || ip == "" || length < 1 {
			vpnClientBad(w, r, "Serveur, port, clés et adresse locale sont obligatoires.")
			return
		}
		wg := map[string]any{"remote_addr": addr, "remote_port": port, "remote_public_key": pub,
			"remote_preshared_key": formStr(r, "remote_preshared_key"), "local_priv_key": priv,
			"local_addr": []map[string]any{{"ip": ip, "len": length}}}
		if dns := strings.Fields(strings.ReplaceAll(formStr(r, "dns"), ",", " ")); len(dns) > 0 {
			wg["dns"] = dns
		}
		if mtu := formInt(r, "mtu"); mtu > 0 {
			wg["mtu"] = mtu
		}
		body["conf_wireguard"] = wg
	default:
		vpnClientBad(w, r, "Type de VPN inconnu.")
		return
	}
	s.change(w, r, http.MethodPost, "vpn_client/config/", body, "Configuration ajoutée (inactive).", "/vpn-client")
}

func (s *Server) vpnClientRename(w http.ResponseWriter, r *http.Request) {
	desc := formStr(r, "description")
	if desc == "" {
		vpnClientBad(w, r, "Le nom ne peut pas être vide.")
		return
	}
	s.change(w, r, http.MethodPut, "vpn_client/config/"+url.PathEscape(r.PathValue("id")),
		map[string]string{"description": desc}, "Configuration renommée.", "/vpn-client")
}

func (s *Server) vpnClientActivate(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/vpn-client") {
		return
	}
	s.change(w, r, http.MethodPut, "vpn_client/config/"+url.PathEscape(r.PathValue("id")),
		map[string]bool{"active": true}, "Configuration activée.", "/vpn-client")
}

func (s *Server) vpnClientDeactivate(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodPut, "vpn_client/config/"+url.PathEscape(r.PathValue("id")),
		map[string]bool{"active": false}, "Configuration désactivée.", "/vpn-client")
}

func (s *Server) vpnClientDelete(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/vpn-client") {
		return
	}
	s.change(w, r, http.MethodDelete, "vpn_client/config/"+url.PathEscape(r.PathValue("id")), nil, "Configuration supprimée.", "/vpn-client")
}
