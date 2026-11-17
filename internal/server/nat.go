package server

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// NAT: port forwarding, DMZ, incoming ports of the box's own services, UPnP IGD.

func init() {
	register(module{Slug: "nat", Title: "Redirections", Group: "Réseau", Order: 20, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /nat", s.nat)
		s.page(mux, "POST /nat/redir", s.addRedir)
		s.page(mux, "POST /nat/redir/{id}/enabled", s.setRedirEnabled)
		s.page(mux, "POST /nat/redir/{id}/delete", s.deleteRedir)
		s.page(mux, "POST /nat/dmz", s.setDMZ)
		s.page(mux, "POST /nat/incoming/{id}", s.setIncoming)
		s.page(mux, "POST /nat/upnp", s.setUPnP)
		s.page(mux, "POST /nat/upnp/redir/{id}/delete", s.deleteUPnPRedir)
	}})
	redirID := ops.Param{Name: "id", Type: "integer", Description: "Port forwarding id"}
	portID := ops.Param{Name: "id", Description: `Incoming port id, e.g. "http", "https", "ftp", "bittorrent-main", "openvpn_routed"`}
	ops.Register(
		ops.Op{Name: "nat_dmz_get", Description: "Get the DMZ setting: enabled and ip of the LAN host that receives all unsolicited incoming traffic.",
			Path: "fw/dmz/"},
		ops.Op{Name: "nat_dmz_set", Description: "Enable, disable or move the DMZ. An enabled DMZ exposes the host to the whole internet.", Method: http.MethodPut,
			Path: "fw/dmz/", Body: true, Dangerous: true, BodyHelp: `{"enabled": true, "ip": "192.168.1.42"}`},
		ops.Op{Name: "nat_redir_list", Description: "List the port forwardings: id, enabled, ip_proto, wan_port_start/end, lan_ip, lan_port, src_ip, comment, hostname.",
			Path: "fw/redir/"},
		ops.Op{Name: "nat_redir_get", Description: "Get one port forwarding.",
			Path: "fw/redir/{id}", Params: []ops.Param{redirID}},
		ops.Op{Name: "nat_redir_add", Description: "Create a port forwarding from the box's public IP to a LAN host. It exposes that port to the internet.", Method: http.MethodPost,
			Path: "fw/redir/", Body: true, Dangerous: true,
			BodyHelp: `{"enabled": true, "comment": "web", "ip_proto": "tcp", "wan_port_start": 8080, "wan_port_end": 8080, "lan_ip": "192.168.1.42", "lan_port": 80, "src_ip": "0.0.0.0"}`},
		ops.Op{Name: "nat_redir_update", Description: "Change a port forwarding (partial update), for example enable or disable it. Enabling exposes the port to the internet.", Method: http.MethodPut,
			Path: "fw/redir/{id}", Params: []ops.Param{redirID}, Body: true, Dangerous: true, BodyHelp: `{"enabled": false}`},
		ops.Op{Name: "nat_redir_delete", Description: "Delete a port forwarding.", Method: http.MethodDelete,
			Path: "fw/redir/{id}", Params: []ops.Param{redirID}},
		ops.Op{Name: "nat_incoming_list", Description: "List the incoming ports the box's own services (remote access, FTP, torrents, VPN servers) bind on the public IP: id, enabled, active, type, in_port, min_port, max_port, readonly.",
			Path: "fw/incoming/"},
		ops.Op{Name: "nat_incoming_get", Description: "Get one incoming port.",
			Path: "fw/incoming/{id}", Params: []ops.Param{portID}},
		ops.Op{Name: "nat_incoming_update", Description: "Enable, disable or move an incoming port. Enabling exposes a service of the box to the internet.", Method: http.MethodPut,
			Path: "fw/incoming/{id}", Params: []ops.Param{portID}, Body: true, Dangerous: true, BodyHelp: `{"enabled": true, "in_port": 3615}`},
		ops.Op{Name: "nat_upnp_config_get", Description: "Get the UPnP IGD settings: enabled, version (1 or 2).",
			Path: "upnpigd/config/"},
		ops.Op{Name: "nat_upnp_config_set", Description: "Enable, disable or change the version of UPnP IGD. Enabled, any LAN host can open ports to the internet.", Method: http.MethodPut,
			Path: "upnpigd/config/", Body: true, Dangerous: true, BodyHelp: `{"enabled": true, "version": 2}`},
		ops.Op{Name: "nat_upnp_redir_list", Description: "List the redirections opened by LAN hosts through UPnP IGD: id, enabled, proto, ext_port, int_ip, int_port, desc, remaining.",
			Path: "upnpigd/redir/"},
		ops.Op{Name: "nat_upnp_redir_delete", Description: "Delete a UPnP IGD redirection.", Method: http.MethodDelete,
			Path: "upnpigd/redir/{id}", Params: []ops.Param{{Name: "id", Description: `UPnP redirection id, e.g. "0.0.0.0-53644-udp"`}}},
	)
}

type natRedir struct {
	ID           int    `json:"id"`
	Enabled      bool   `json:"enabled"`
	Comment      string `json:"comment"`
	Proto        string `json:"ip_proto"`
	WanPortStart int    `json:"wan_port_start"`
	WanPortEnd   int    `json:"wan_port_end"`
	LanIP        string `json:"lan_ip"`
	LanPort      int    `json:"lan_port"`
	SrcIP        string `json:"src_ip"`
	Hostname     string `json:"hostname"`
}

// WanPorts is the public port or range, as people write it.
func (r natRedir) WanPorts() string {
	if r.WanPortEnd > r.WanPortStart {
		return strconv.Itoa(r.WanPortStart) + "–" + strconv.Itoa(r.WanPortEnd)
	}
	return strconv.Itoa(r.WanPortStart)
}

// LanPorts is the target port or range on the LAN host.
func (r natRedir) LanPorts() string {
	if n := r.WanPortEnd - r.WanPortStart; n > 0 {
		return strconv.Itoa(r.LanPort) + "–" + strconv.Itoa(r.LanPort+n)
	}
	return strconv.Itoa(r.LanPort)
}

// AnySource is true when the rule applies to every source address.
func (r natRedir) AnySource() bool { return r.SrcIP == "" || r.SrcIP == "0.0.0.0" }

type natIncoming struct {
	ID       string `json:"id"`
	Enabled  bool   `json:"enabled"`
	Active   bool   `json:"active"`
	Type     string `json:"type"`
	InPort   int    `json:"in_port"`
	MinPort  int    `json:"min_port"`
	MaxPort  int    `json:"max_port"`
	ReadOnly bool   `json:"readonly"`
}

// Label says what the incoming port is for.
func (p natIncoming) Label() string {
	if l, ok := incomingLabels[p.ID]; ok {
		return l
	}
	return p.ID
}

var incomingLabels = map[string]string{
	"http":            "Accès distant Freebox OS (HTTP)",
	"https":           "Accès distant Freebox OS (HTTPS)",
	"bittorrent-main": "BitTorrent (principal)",
	"bittorrent-dht":  "BitTorrent (DHT)",
	"openvpn_routed":  "OpenVPN routé",
	"openvpn_bridge":  "OpenVPN en pont",
	"ipsec_ike":       "IPsec IKEv2",
	"ipsec_nat":       "IPsec NAT",
	"pptp":            "PPTP",
	"ftp":             "FTP (contrôle)",
	"ftp_pasv":        "FTP (données)",
}

type natDMZ struct {
	Enabled bool   `json:"enabled"`
	IP      string `json:"ip"`
}

type natUPnPConfig struct {
	Enabled bool `json:"enabled"`
	Version int  `json:"version"`
}

type natUPnPRedir struct {
	ID        string `json:"id"`
	Enabled   bool   `json:"enabled"`
	Proto     string `json:"proto"`
	ExtPort   int    `json:"ext_port"`
	IntIP     string `json:"int_ip"`
	IntPort   int    `json:"int_port"`
	Desc      string `json:"desc"`
	Remaining int64  `json:"remaining"`
}

func (s *Server) nat(w http.ResponseWriter, r *http.Request) {
	var (
		redirs   []natRedir
		incoming []natIncoming
		dmz      natDMZ
		upnp     natUPnPConfig
		leases   []natUPnPRedir
	)
	var err error
	for _, f := range []struct {
		path string
		out  any
	}{{"fw/redir/", &redirs}, {"fw/dmz/", &dmz}, {"fw/incoming/", &incoming}, {"upnpigd/config/", &upnp}, {"upnpigd/redir/", &leases}} {
		if err = s.fetch(r.Context(), f.path, f.out); err != nil {
			break
		}
	}
	sort.Slice(redirs, func(i, j int) bool { return redirs[i].ID < redirs[j].ID })
	sort.Slice(incoming, func(i, j int) bool { return incoming[i].Label() < incoming[j].Label() })
	s.show(w, r, "nat", map[string]any{"Redirs": redirs, "Incoming": incoming, "DMZ": dmz, "UPnP": upnp, "UPnPRedirs": leases}, err)
}

func (s *Server) addRedir(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/nat") {
		return
	}
	proto := formStr(r, "ip_proto")
	start, end, lan := formInt(r, "wan_port_start"), formInt(r, "wan_port_end"), formInt(r, "lan_port")
	if end == 0 {
		end = start
	}
	src := formStr(r, "src_ip")
	if src == "" {
		src = "0.0.0.0"
	}
	switch {
	case proto != "tcp" && proto != "udp":
		dhcpRefuse(w, r, "/nat", "Protocole invalide.")
	case start < 1 || end > 65535 || end < start:
		dhcpRefuse(w, r, "/nat", "Ports externes invalides.")
	case lan < 1 || lan+(end-start) > 65535:
		dhcpRefuse(w, r, "/nat", "Port de destination invalide.")
	case !dhcpIPv4(formStr(r, "lan_ip")) || !dhcpIPv4(src):
		dhcpRefuse(w, r, "/nat", "Adresse IPv4 invalide.")
	default:
		s.change(w, r, http.MethodPost, "fw/redir/", map[string]any{
			"enabled": true, "comment": formStr(r, "comment"), "ip_proto": proto,
			"wan_port_start": start, "wan_port_end": end, "lan_ip": formStr(r, "lan_ip"), "lan_port": lan, "src_ip": src,
		}, "Redirection ajoutée.", "/nat")
	}
}

// setRedirEnabled enables (needs confirmation: it exposes a port) or disables a forwarding.
func (s *Server) setRedirEnabled(w http.ResponseWriter, r *http.Request) {
	on := formBool(r, "enabled")
	if on && !confirmed(w, r, "/nat") {
		return
	}
	msg := "Redirection désactivée."
	if on {
		msg = "Redirection activée."
	}
	s.change(w, r, http.MethodPut, "fw/redir/"+url.PathEscape(r.PathValue("id")), map[string]bool{"enabled": on}, msg, "/nat")
}

func (s *Server) deleteRedir(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodDelete, "fw/redir/"+url.PathEscape(r.PathValue("id")), nil, "Redirection supprimée.", "/nat")
}

func (s *Server) setDMZ(w http.ResponseWriter, r *http.Request) {
	on, ip := formBool(r, "enabled"), formStr(r, "ip")
	if on {
		if !dhcpIPv4(ip) {
			dhcpRefuse(w, r, "/nat", "Adresse IPv4 invalide.")
			return
		}
		if !confirmed(w, r, "/nat") {
			return
		}
	}
	body := map[string]any{"enabled": on}
	if ip != "" {
		body["ip"] = ip
	}
	msg := "DMZ désactivée."
	if on {
		msg = "DMZ activée."
	}
	s.change(w, r, http.MethodPut, "fw/dmz/", body, msg, "/nat")
}

// setIncoming enables or moves an incoming port; enabling needs confirmation.
func (s *Server) setIncoming(w http.ResponseWriter, r *http.Request) {
	on := formBool(r, "enabled")
	if on && !confirmed(w, r, "/nat") {
		return
	}
	body := map[string]any{"enabled": on}
	if p := formInt(r, "in_port"); p > 0 {
		if p > 65535 {
			dhcpRefuse(w, r, "/nat", "Port invalide.")
			return
		}
		body["in_port"] = p
	}
	s.change(w, r, http.MethodPut, "fw/incoming/"+url.PathEscape(r.PathValue("id")), body, "Port entrant enregistré.", "/nat")
}

// setUPnP enables UPnP IGD (needs confirmation: any LAN host can then open
// ports) or disables it.
func (s *Server) setUPnP(w http.ResponseWriter, r *http.Request) {
	on := formBool(r, "enabled")
	if on && !confirmed(w, r, "/nat") {
		return
	}
	body := map[string]any{"enabled": on}
	if v := formInt(r, "version"); v == 1 || v == 2 {
		body["version"] = v
	}
	s.change(w, r, http.MethodPut, "upnpigd/config/", body, "Réglages UPnP enregistrés.", "/nat")
}

func (s *Server) deleteUPnPRedir(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodDelete, "upnpigd/redir/"+url.PathEscape(r.PathValue("id")), nil, "Redirection UPnP supprimée.", "/nat")
}
