package server

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"rsc.io/qr"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// VPN server: WireGuard, OpenVPN, PPTP and IPsec servers of the box, their
// users, the IP pool and the live connections.

func init() {
	register(module{Slug: "vpn", Title: "Serveur VPN", Group: "VPN", Order: 0, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /vpn", s.vpn)
		s.page(mux, "POST /vpn/{name}/start", s.vpnStart)
		s.page(mux, "POST /vpn/{name}/stop", s.vpnStop)
		s.page(mux, "POST /vpn/{name}/port", s.vpnPort)
		s.page(mux, "POST /vpn/{name}/settings", s.vpnSettings)
		s.page(mux, "POST /vpn/users", s.vpnUserAdd)
		s.page(mux, "POST /vpn/users/{login}/update", s.vpnUserUpdate)
		s.page(mux, "POST /vpn/users/{login}/delete", s.vpnUserDelete)
		s.page(mux, "POST /vpn/connections/{id}/close", s.vpnConnClose)
		s.page(mux, "GET /vpn/config/{server}/{login}", s.vpnDownload)
		s.page(mux, "POST /vpn/config/{server}/{login}", s.vpnDownload)
		s.page(mux, "GET /vpn/qr/{server}/{login}", s.vpnQR)
	}})
	userBody := `{"login": "phone", "type": "wireguard", "ip_reservation": "10.8.0.70", "conf_wireguard": {"keepalive": 15, "psk": false}}`
	ops.Register(
		ops.Op{Name: "vpn_servers_list", Description: "List the VPN servers (pptp, openvpn_routed, openvpn_bridge, ipsec, wireguard): name, type, state (stopped/starting/started/stopping/error), connection_count.",
			Path: "vpn/"},
		ops.Op{Name: "vpn_server_config_get", Secret: true, Description: "Get a VPN server's configuration: enabled, port (+ min_port/max_port), IP range, and conf_wireguard (mtu) / conf_openvpn (cipher, use_tcp, disable_fragment) / conf_pptp (mppe, allowed_auth).",
			Path: "vpn/{vpn_id}/config/", Params: []ops.Param{{Name: "vpn_id", Description: `Server name, e.g. "wireguard" or "openvpn_routed"`}}},
		ops.Op{Name: "vpn_server_config_update", Description: "Update a VPN server: start or stop it (enabled), change its port or its protocol settings. Stopping the server or changing its port disconnects every VPN client.", Method: http.MethodPut,
			Path: "vpn/{vpn_id}/config/", Params: []ops.Param{{Name: "vpn_id", Description: `Server name, e.g. "wireguard"`}},
			Body: true, BodyHelp: `{"enabled": true, "port": 51820, "conf_wireguard": {"mtu": 1360}}`, Dangerous: true},
		ops.Op{Name: "vpn_users_list", Secret: true, Description: "List the VPN users, common to all servers: login, type (standard or wireguard), ip_reservation, password_set, conf_wireguard (keepalive, psk).",
			Path: "vpn/user/"},
		ops.Op{Name: "vpn_user_get", Secret: true, Description: "Get one VPN user by login.",
			Path: "vpn/user/{login}", Params: []ops.Param{{Name: "login", Description: "VPN user login"}}},
		ops.Op{Name: "vpn_user_add", Description: "Create a VPN user. A wireguard user needs an ip_reservation inside the VPN pool; a standard user (OpenVPN, PPTP, IPsec) needs a password of 8 to 32 characters.", Method: http.MethodPost,
			Path: "vpn/user/", Body: true, BodyHelp: userBody, Dangerous: true},
		ops.Op{Name: "vpn_user_update", Description: "Update a VPN user: password, ip_reservation or conf_wireguard.", Method: http.MethodPut,
			Path: "vpn/user/{login}", Params: []ops.Param{{Name: "login", Description: "VPN user login"}},
			Body: true, BodyHelp: `{"ip_reservation": "10.8.0.71", "conf_wireguard": {"keepalive": 25}}`, Dangerous: true},
		ops.Op{Name: "vpn_user_delete", Description: "Delete a VPN user; their devices can no longer connect and any configuration file given to them stops working.", Method: http.MethodDelete,
			Path: "vpn/user/{login}", Params: []ops.Param{{Name: "login", Description: "VPN user login"}}, Dangerous: true},
		ops.Op{Name: "vpn_ip_pool_get", Description: "Get the VPN IP pool: ip_start, ip_end and the reservations (login, ip).",
			Path: "vpn/ip_pool/"},
		ops.Op{Name: "vpn_connections_list", Description: "List the active VPN connections: id, vpn (server), user, authenticated, src_ip, local_ip, rx_bytes, tx_bytes, auth_time.",
			Path: "vpn/connection/"},
		ops.Op{Name: "vpn_connection_close", Description: "Close one active VPN connection by id (the client may reconnect).", Method: http.MethodDelete,
			Path: "vpn/connection/{id}", Params: []ops.Param{{Name: "id", Description: `Connection id, e.g. "wireguard-60"`}}, Dangerous: true},
		ops.Op{Name: "vpn_config_download", Secret: true, Description: "Download a user's client configuration file for an OpenVPN or WireGuard server; with fmt=json the file comes back as a string. SECRET: contains the user's private key. Each OpenVPN download invalidates the previous file of that user. Needs the settings permission.",
			Path: "vpn/download_config/{server_name}/{login}/{fmt}", Params: []ops.Param{
				{Name: "server_name", Description: `Server name, e.g. "wireguard"`}, {Name: "login", Description: "VPN user login"},
				{Name: "fmt", Description: `Use "json"`}}, Dangerous: true},
	)
}

type vpnServer struct {
	Name                string           `json:"name"`
	Type                string           `json:"type"`
	State               string           `json:"state"`
	ConnectionCount     int              `json:"connection_count"`
	AuthConnectionCount int              `json:"auth_connection_count"`
	Cfg                 *vpnServerConfig `json:"-"`
}

// Label is the server's name as the box UI shows it.
func (v vpnServer) Label() string {
	switch v.Name {
	case "openvpn_routed":
		return "OpenVPN (routé)"
	case "openvpn_bridge":
		return "OpenVPN (pont)"
	}
	switch v.Type {
	case "wireguard":
		return "WireGuard"
	case "pptp":
		return "PPTP"
	case "ipsec":
		return "IPsec IKEv2"
	}
	return v.Name
}

// Started tells whether the server is running or about to.
func (v vpnServer) Started() bool { return v.State == "started" || v.State == "starting" }

// StateLabel is the state in French.
func (v vpnServer) StateLabel() string {
	switch v.State {
	case "stopped":
		return "arrêté"
	case "starting":
		return "démarrage en cours"
	case "started":
		return "en marche"
	case "stopping":
		return "arrêt en cours"
	case "error":
		return "erreur"
	}
	return v.State
}

// HasPort tells whether the box lets the port be edited.
func (v vpnServer) HasPort() bool { return v.Type == "wireguard" || v.Type == "openvpn" }

type vpnServerConfig struct {
	Enabled    bool   `json:"enabled"`
	Port       int    `json:"port"`
	MinPort    int    `json:"min_port"`
	MaxPort    int    `json:"max_port"`
	PortIKE    int    `json:"port_ike"`
	PortNAT    int    `json:"port_nat"`
	EnableIPv4 bool   `json:"enable_ipv4"`
	EnableIPv6 bool   `json:"enable_ipv6"`
	IPStart    string `json:"ip_start"`
	IPEnd      string `json:"ip_end"`
	ConfWG     struct {
		MTU int `json:"mtu"`
	} `json:"conf_wireguard"`
	ConfOpenVPN struct {
		Cipher          string `json:"cipher"`
		DisableFragment bool   `json:"disable_fragment"`
		UseTCP          bool   `json:"use_tcp"`
	} `json:"conf_openvpn"`
	ConfPPTP struct {
		MPPE        string          `json:"mppe"`
		AllowedAuth map[string]bool `json:"allowed_auth"`
	} `json:"conf_pptp"`
}

type vpnUser struct {
	Login         string `json:"login"`
	Type          string `json:"type"`
	IPReservation string `json:"ip_reservation"`
	PasswordSet   bool   `json:"password_set"`
	ConfWG        struct {
		Keepalive int  `json:"keepalive"`
		PSK       bool `json:"psk"`
	} `json:"conf_wireguard"`
}

type vpnConn struct {
	ID            string `json:"id"`
	VPN           string `json:"vpn"`
	User          string `json:"user"`
	Authenticated bool   `json:"authenticated"`
	AuthTime      int64  `json:"auth_time"`
	SrcIP         string `json:"src_ip"`
	LocalIP       string `json:"local_ip"`
	RX            int64  `json:"rx_bytes"`
	TX            int64  `json:"tx_bytes"`
}

type vpnPool struct {
	IPStart      string `json:"ip_start"`
	IPEnd        string `json:"ip_end"`
	Reservations []struct {
		Login string `json:"login"`
		IP    string `json:"ip"`
	} `json:"reservations"`
}

func (s *Server) vpn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var servers []vpnServer
	if err := s.fetch(ctx, "vpn/", &servers); err != nil {
		s.show(w, r, "vpn", nil, err)
		return
	}
	wg, openvpn := "", []string{}
	for i := range servers {
		var cfg vpnServerConfig
		if s.fetch(ctx, "vpn/"+url.PathEscape(servers[i].Name)+"/config/", &cfg) == nil {
			servers[i].Cfg = &cfg
		}
		switch servers[i].Type {
		case "wireguard":
			if wg == "" {
				wg = servers[i].Name
			}
		case "openvpn":
			openvpn = append(openvpn, servers[i].Name)
		}
	}
	var users []vpnUser
	var conns []vpnConn
	var pool vpnPool
	err := s.fetch(ctx, "vpn/user/", &users)
	if e := s.fetch(ctx, "vpn/connection/", &conns); err == nil {
		err = e
	}
	if e := s.fetch(ctx, "vpn/ip_pool/", &pool); err == nil {
		err = e
	}
	sort.Slice(users, func(i, j int) bool { return strings.ToLower(users[i].Login) < strings.ToLower(users[j].Login) })
	s.show(w, r, "vpn", map[string]any{"Servers": servers, "Users": users, "Conns": conns, "Pool": pool,
		"WG": wg, "OpenVPN": openvpn}, err)
}

func (s *Server) vpnSetEnabled(w http.ResponseWriter, r *http.Request, on bool, done string) {
	s.change(w, r, http.MethodPut, "vpn/"+url.PathEscape(r.PathValue("name"))+"/config/",
		map[string]any{"enabled": on}, done, "/vpn")
}

func (s *Server) vpnStart(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/vpn") {
		return
	}
	s.vpnSetEnabled(w, r, true, "Serveur démarré.")
}

func (s *Server) vpnStop(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/vpn") {
		return
	}
	s.vpnSetEnabled(w, r, false, "Serveur arrêté.")
}

func (s *Server) vpnPort(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/vpn") {
		return
	}
	port := formInt(r, "port")
	if port < 1 || port > 65535 {
		setFlash(w, "err:Le port doit être compris entre 1 et 65535.")
		http.Redirect(w, r, "/vpn", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "vpn/"+url.PathEscape(r.PathValue("name"))+"/config/",
		map[string]any{"port": port}, "Port modifié.", "/vpn")
}

// vpnSettings saves the protocol settings of one server; the fields sent
// depend on the server type (given in the form).
func (s *Server) vpnSettings(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/vpn") {
		return
	}
	body := map[string]any{}
	switch formStr(r, "type") {
	case "wireguard":
		mtu := formInt(r, "mtu")
		if mtu < 512 || mtu > 1420 {
			setFlash(w, "err:Le MTU doit être compris entre 512 et 1420.")
			http.Redirect(w, r, "/vpn", http.StatusSeeOther)
			return
		}
		body["conf_wireguard"] = map[string]any{"mtu": mtu}
	case "openvpn":
		body["conf_openvpn"] = map[string]any{"cipher": formStr(r, "cipher"),
			"use_tcp": formBool(r, "use_tcp"), "disable_fragment": formBool(r, "disable_fragment")}
	case "pptp":
		body["conf_pptp"] = map[string]any{"mppe": formStr(r, "mppe"), "allowed_auth": map[string]bool{
			"pap": formBool(r, "pap"), "chap": formBool(r, "chap"), "mschapv2": formBool(r, "mschapv2")}}
	default:
		setFlash(w, "err:Type de serveur inconnu.")
		http.Redirect(w, r, "/vpn", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "vpn/"+url.PathEscape(r.PathValue("name"))+"/config/", body, "Réglages enregistrés.", "/vpn")
}

// vpnBad flashes a form error and goes back to the page.
func vpnBad(w http.ResponseWriter, r *http.Request, msg string) {
	setFlash(w, "err:"+msg)
	http.Redirect(w, r, "/vpn", http.StatusSeeOther)
}

func (s *Server) vpnUserAdd(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/vpn") {
		return
	}
	login, typ := formStr(r, "login"), formStr(r, "type")
	if login == "" {
		vpnBad(w, r, "Le nom ne peut pas être vide.")
		return
	}
	body := map[string]any{"login": login, "type": typ, "ip_reservation": formStr(r, "ip_reservation")}
	switch typ {
	case "wireguard":
		if body["ip_reservation"] == "" {
			vpnBad(w, r, "Un utilisateur WireGuard a besoin d'une IP réservée dans le réseau VPN.")
			return
		}
		body["conf_wireguard"] = map[string]any{"keepalive": formInt(r, "keepalive"), "psk": formBool(r, "psk")}
	case "standard":
		pw := r.PostFormValue("password")
		if len(pw) < 8 || len(pw) > 32 {
			vpnBad(w, r, "Le mot de passe doit faire entre 8 et 32 caractères.")
			return
		}
		body["password"] = pw
	default:
		vpnBad(w, r, "Type d'utilisateur inconnu.")
		return
	}
	s.change(w, r, http.MethodPost, "vpn/user/", body, "Utilisateur ajouté.", "/vpn")
}

func (s *Server) vpnUserUpdate(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/vpn") {
		return
	}
	body := map[string]any{}
	if ip := formStr(r, "ip_reservation"); ip != "" {
		body["ip_reservation"] = ip
	}
	if pw := r.PostFormValue("password"); pw != "" {
		if len(pw) < 8 || len(pw) > 32 {
			vpnBad(w, r, "Le mot de passe doit faire entre 8 et 32 caractères.")
			return
		}
		body["password"] = pw
	}
	if ka := formInt(r, "keepalive"); ka > 0 {
		body["conf_wireguard"] = map[string]any{"keepalive": ka, "psk": formBool(r, "psk")}
	}
	if len(body) == 0 {
		vpnBad(w, r, "Rien à modifier.")
		return
	}
	s.change(w, r, http.MethodPut, "vpn/user/"+url.PathEscape(r.PathValue("login")), body, "Utilisateur modifié.", "/vpn")
}

func (s *Server) vpnUserDelete(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/vpn") {
		return
	}
	s.change(w, r, http.MethodDelete, "vpn/user/"+url.PathEscape(r.PathValue("login")), nil, "Utilisateur supprimé.", "/vpn")
}

func (s *Server) vpnConnClose(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/vpn") {
		return
	}
	s.change(w, r, http.MethodDelete, "vpn/connection/"+url.PathEscape(r.PathValue("id")), nil, "Connexion fermée.", "/vpn")
}

// clientConfig fetches a user's client file. It is a secret: callers must
// never log it.
func (s *Server) clientConfig(r *http.Request, server, login string) (string, error) {
	var text string
	err := s.fetch(r.Context(), "vpn/download_config/"+url.PathEscape(server)+"/"+url.PathEscape(login)+"/json", &text)
	if err != nil {
		return "", fmt.Errorf("download VPN configuration: %w", err)
	}
	return text, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// vpnDownload sends the configuration file as an attachment. GET is for
// WireGuard (the file is regenerated identically); an OpenVPN download
// invalidates the previous file, so it only answers a confirmed POST.
func (s *Server) vpnDownload(w http.ResponseWriter, r *http.Request) {
	server, login := r.PathValue("server"), r.PathValue("login")
	if r.Method == http.MethodPost {
		if !confirmed(w, r, "/vpn") {
			return
		}
	} else if !strings.HasPrefix(server, "wireguard") {
		http.Error(w, "Le téléchargement d'un fichier OpenVPN se fait par un formulaire confirmé.", http.StatusMethodNotAllowed)
		return
	}
	text, err := s.clientConfig(r, server, login)
	if err != nil {
		s.log.Error("vpn download", "server", server, "err", err)
		vpnBad(w, r, err.Error())
		return
	}
	ext := ".conf"
	if strings.HasPrefix(server, "openvpn") {
		ext = ".ovpn"
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", `attachment; filename="`+unsafeName.ReplaceAllString(server+"_"+login, "_")+ext+`"`)
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(text))
}

// vpnQR draws a WireGuard configuration as a QR code to scan from the
// WireGuard mobile app.
func (s *Server) vpnQR(w http.ResponseWriter, r *http.Request) {
	server, login := r.PathValue("server"), r.PathValue("login")
	if !strings.HasPrefix(server, "wireguard") {
		http.NotFound(w, r)
		return
	}
	text, err := s.clientConfig(r, server, login)
	if err != nil {
		s.log.Error("vpn qr", "server", server, "err", err)
		http.Error(w, "La box n'a pas fourni la configuration.", http.StatusBadGateway)
		return
	}
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		s.log.Error("vpn qr", "server", server, "err", err)
		http.Error(w, "Configuration trop longue pour un QR code.", http.StatusUnprocessableEntity)
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, qrImage(code)); err != nil {
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "no-store")
	_, _ = buf.WriteTo(w)
}

// qrImage scales a QR code up and adds the 4-module quiet zone.
func qrImage(c *qr.Code) image.Image {
	const scale, quiet = 6, 4
	n := c.Size + 2*quiet
	img := image.NewGray(image.Rect(0, 0, n*scale, n*scale))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	for y := range c.Size {
		for x := range c.Size {
			if !c.Black(x, y) {
				continue
			}
			for dy := range scale {
				for dx := range scale {
					img.SetGray((x+quiet)*scale+dx, (y+quiet)*scale+dy, color.Gray{})
				}
			}
		}
	}
	return img
}
