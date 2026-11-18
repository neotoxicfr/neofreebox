package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Connection: the internet line in detail (FTTH optics, xDSL, 4G backup),
// the connection settings (ping, remote access, IPv6) and DynDNS. The live
// state and rates are on the status page.

func init() {
	register(module{Slug: "connection", Title: "Connexion internet", Group: "Réseau", Order: 30, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /connection", s.connection)
		s.page(mux, "POST /connection/config", s.connectionConfig)
		s.page(mux, "POST /connection/remote", s.connectionRemote)
		s.page(mux, "POST /connection/ipv6", s.connectionIPv6)
		s.page(mux, "POST /connection/ddns/{provider}", s.connectionDDNS)
	}})
	provider := ops.Param{Name: "provider", Description: `DynDNS provider: "ovh", "dyndns" or "noip"`}
	ops.Register(
		ops.Op{Name: "connection_status_get", Description: "Get the internet connection status: state, type, media (ftth/xdsl/ethernet/backup_4g), IPv4/IPv6, rates (bytes/s), bandwidth (bit/s), ipv4_port_range.",
			Path: "connection/"},
		ops.Op{Name: "connection_config_get", Description: "Get the connection settings: ping, wol (Wake-on-LAN proxy), adblock, remote_access and port, is_secure_pass, allow_token_request, sip_alg.",
			Path: "connection/config/"},
		ops.Op{Name: "connection_config_update", Description: "Change connection settings. Enabling remote_access exposes the Freebox admin to the internet (needs a secure admin password). sip_alg is \"direct_media\" (SIP ALG on, media only between SIP endpoints: the strictest mode a Freebox Pop accepts) or \"any_media\" (factory default); \"disabled\" is documented but refused by a Freebox Pop (invalid_sip_alg).",
			Method: http.MethodPut, Path: "connection/config/", Body: true, Dangerous: true,
			BodyHelp: `{"ping": true, "wol": false, "adblock": false, "allow_token_request": true, "sip_alg": "direct_media", "remote_access": false, "remote_access_port": 44666}`},
		ops.Op{Name: "connection_ipv6_config_get", Description: "Get the IPv6 settings: ipv6_enabled, ipv6_firewall, ipv6_prefix_firewall, ipv6ll and the 8 delegated /64 prefixes with their next_hop.",
			Path: "connection/ipv6/config/"},
		ops.Op{Name: "connection_ipv6_config_update", Description: "Change IPv6 settings (enable, firewalls, next hop of delegated prefixes). Disabling IPv6 or its firewall can cut or expose hosts.",
			Method: http.MethodPut, Path: "connection/ipv6/config/", Body: true, Dangerous: true,
			BodyHelp: `{"ipv6_enabled": true, "ipv6_firewall": true, "ipv6_prefix_firewall": true, "delegations": [{"prefix": "2001:db8:0:1::/64", "next_hop": "fe80::1"}]}`},
		ops.Op{Name: "connection_ftth_get", Description: "Get the FTTH optical module status: sfp_present, sfp_has_signal, link, model/vendor/serial, sfp_pwr_rx and sfp_pwr_tx (dBm x 100).",
			Path: "connection/ftth/"},
		ops.Op{Name: "connection_xdsl_get", Description: "Get the xDSL line status and per-direction stats (rate and maxrate in kbit/s, snr, attn, errors).",
			Path: "connection/xdsl/"},
		ops.Op{Name: "connection_lte_get", Description: "Get a 4G module status (state, radio bands and signal, network, SIM).",
			Path: "connection/lte/{id}", Params: []ops.Param{{Name: "id", Description: `"backup" or "aggregation"`}}},
		ops.Op{Name: "connection_aggregation_get", Description: "Get the xDSL/4G aggregation status (enabled and tunnel rates).",
			Path: "connection/aggregation"},
		ops.Op{Name: "connection_aggregation_update", Description: "Enable or disable xDSL/4G aggregation.", Method: http.MethodPut,
			Path: "connection/aggregation", Body: true, Dangerous: true, BodyHelp: `{"enabled": true}`},
		ops.Op{Name: "connection_ddns_status_get", Description: "Get the status of a DynDNS service: status, last/next refresh timestamps.",
			Path: "connection/ddns/{provider}/status/", Params: []ops.Param{provider}},
		ops.Op{Name: "connection_ddns_get", Description: "Get the configuration of a DynDNS service: enabled, hostname, user (the password is never returned).",
			Path: "connection/ddns/{provider}/", Params: []ops.Param{provider}},
		ops.Op{Name: "connection_ddns_update", Description: "Set the configuration of a DynDNS service.", Method: http.MethodPut,
			Path: "connection/ddns/{provider}/", Params: []ops.Param{provider}, Body: true,
			BodyHelp: `{"enabled": true, "hostname": "example.dyndns.org", "user": "name", "password": "secret"}`},
	)
}

type connStatus struct {
	State string `json:"state"`
	Type  string `json:"type"`
	Media string `json:"media"`
	IPv4  string `json:"ipv4"`
	IPv6  string `json:"ipv6"`
	Ports []int  `json:"ipv4_port_range"`
}

// MediaLabel is the line type in French.
func (c connStatus) MediaLabel() string {
	switch c.Media {
	case "ftth":
		return "Fibre (FTTH)"
	case "xdsl":
		return "xDSL"
	case "ethernet":
		return "Ethernet"
	case "backup_4g":
		return "Secours 4G"
	}
	return c.Media
}

// StateLabel is the line state in French.
func (c connStatus) StateLabel() string { return lineState(c.State) }

// lineState names a connection state the way the page says it.
func lineState(s string) string {
	switch s {
	case "up":
		return "connecté"
	case "down":
		return "déconnecté"
	case "going_up":
		return "connexion en cours"
	case "going_down":
		return "déconnexion en cours"
	}
	return s
}

type connConfig struct {
	Ping         bool   `json:"ping"`
	WOL          bool   `json:"wol"`
	Adblock      bool   `json:"adblock"`
	AllowToken   bool   `json:"allow_token_request"`
	SIPALG       string `json:"sip_alg"`
	RemoteAccess bool   `json:"remote_access"`
	RemotePort   int    `json:"remote_access_port"`
	RemoteMin    int    `json:"remote_access_min_port"`
	RemoteMax    int    `json:"remote_access_max_port"`
	RemoteIP     string `json:"remote_access_ip"`
	SecurePass   bool   `json:"is_secure_pass"`
	APIRemote    bool   `json:"api_remote_access"`
	APIDomain    string `json:"api_domain"`
}

type ipv6Config struct {
	Enabled        bool   `json:"ipv6_enabled"`
	Firewall       bool   `json:"ipv6_firewall"`
	PrefixFirewall bool   `json:"ipv6_prefix_firewall"`
	LinkLocal      string `json:"ipv6ll"`
	Delegations    []struct {
		Prefix  string `json:"prefix"`
		NextHop string `json:"next_hop"`
	} `json:"delegations"`
}

type ftthStatus struct {
	Present   bool   `json:"sfp_present"`
	AlimOK    bool   `json:"sfp_alim_ok"`
	HasSignal bool   `json:"sfp_has_signal"`
	HasPower  bool   `json:"sfp_has_power_report"`
	Link      bool   `json:"link"`
	LinkType  string `json:"link_type"`
	Model     string `json:"sfp_model"`
	Vendor    string `json:"sfp_vendor"`
	Serial    string `json:"sfp_serial"`
	PwrRx     int    `json:"sfp_pwr_rx"`
	PwrTx     int    `json:"sfp_pwr_tx"`
}

// Tech names the optical technology in French.
func (f ftthStatus) Tech() string {
	switch f.LinkType {
	case "pon", "gpon", "xgs_pon":
		return "fibre partagée (PON)"
	case "p2p", "ftth_p2p":
		return "fibre dédiée (point à point)"
	}
	return f.LinkType
}

// Rx and Tx are the optical powers; the box gives dBm x 100.
func (f ftthStatus) Rx() string { return fmt.Sprintf("%.2f dBm", float64(f.PwrRx)/100) }
func (f ftthStatus) Tx() string { return fmt.Sprintf("%.2f dBm", float64(f.PwrTx)/100) }

type xdslStats struct {
	Rate    int  `json:"rate"`
	MaxRate int  `json:"maxrate"`
	SNR     int  `json:"snr"`
	Attn    int  `json:"attn"`
	CRC     int  `json:"crc"`
	FEC     int  `json:"fec"`
	HEC     int  `json:"hec"`
	ES      int  `json:"es"`
	SES     int  `json:"ses"`
	PhyR    bool `json:"phyr"`
}

type xdslInfos struct {
	Status struct {
		Status     string `json:"status"`
		Protocol   string `json:"protocol"`
		Modulation string `json:"modulation"`
		Uptime     int64  `json:"uptime"`
	} `json:"status"`
	Down xdslStats `json:"down"`
	Up   xdslStats `json:"up"`
}

type lteInfos struct {
	Enabled bool   `json:"enabled"`
	State   string `json:"state"`
	Radio   struct {
		Associated  bool `json:"associated"`
		SignalLevel int  `json:"signal_level"`
	} `json:"radio"`
	Network struct {
		IPv4 string `json:"ipv4"`
		IPv6 string `json:"ipv6"`
	} `json:"network"`
	SIM struct {
		Present bool `json:"present"`
	} `json:"sim"`
}

// StateLabel is the 4G module state in French.
func (l lteInfos) StateLabel() string {
	switch l.State {
	case "idle":
		return "en veille"
	case "connected":
		return "connecté"
	case "disconnected":
		return "déconnecté"
	case "connecting":
		return "connexion en cours"
	case "not_detected":
		return "non détecté"
	}
	return l.State
}

type ddnsConfig struct {
	Enabled  bool   `json:"enabled"`
	Hostname string `json:"hostname"`
	User     string `json:"user"`
}

type ddnsStatus struct {
	Status      string `json:"status"`
	LastRefresh int64  `json:"last_refresh"`
	NextRefresh int64  `json:"next_refresh"`
}

// Label is the DynDNS status in French.
func (d ddnsStatus) Label() string {
	if l, ok := ddnsLabels[d.Status]; ok {
		return l
	}
	return d.Status
}

var ddnsLabels = map[string]string{
	"disabled": "Désactivé", "ok": "À jour", "wait": "Mise à jour en cours", "reqfail": "Requête échouée",
	"authfail": "Authentification refusée", "nocredential": "Identifiants invalides", "ipinval": "IP invalide",
	"hostinval": "Nom d'hôte invalide", "abuse": "Bloqué pour abus", "dnserror": "Erreur DNS",
	"unavailable": "Service indisponible", "nowan": "IP publique introuvable", "unknown": "Inconnu",
}

// Tone is the badge class of the provider: nothing when off, on when up to
// date, warn while updating, off for a failure.
func (p ddnsProvider) Tone() string {
	switch {
	case !p.Config.Enabled:
		return ""
	case p.Status.Status == "ok":
		return "on"
	case p.Status.Status == "wait":
		return "warn"
	}
	return "off"
}

// StateLabel is the badge text: off, or the update status.
func (p ddnsProvider) StateLabel() string {
	if !p.Config.Enabled {
		return "désactivé"
	}
	return p.Status.Label()
}

type ddnsProvider struct {
	ID, Label string
	Config    ddnsConfig
	Status    ddnsStatus
	Err       string
}

var ddnsProviders = []ddnsProvider{{ID: "ovh", Label: "OVH"}, {ID: "dyndns", Label: "DynDNS"}, {ID: "noip", Label: "No-IP"}}

func (s *Server) connection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var conn connStatus
	var cfg connConfig
	err := s.fetch(ctx, "connection/", &conn)
	if err == nil {
		err = s.fetch(ctx, "connection/config/", &cfg)
	}
	if err != nil {
		s.show(w, r, "connection", nil, err)
		return
	}
	data := map[string]any{"Conn": conn, "Cfg": cfg}
	// The rest is optional: a box without 4G, or a failing endpoint, must not hide the page.
	var v6 ipv6Config
	if s.fetch(ctx, "connection/ipv6/config/", &v6) == nil {
		data["V6"] = &v6
	}
	if conn.Media == "ftth" {
		var f ftthStatus
		if s.fetch(ctx, "connection/ftth/", &f) == nil {
			data["Ftth"] = &f
		}
	}
	if conn.Media == "xdsl" {
		var x xdslInfos
		if s.fetch(ctx, "connection/xdsl/", &x) == nil {
			data["Xdsl"] = &x
		}
	}
	var lte lteInfos
	if s.fetch(ctx, "connection/lte/backup", &lte) == nil && lte.State != "not_detected" {
		data["Lte"] = &lte
	}
	providers := make([]ddnsProvider, len(ddnsProviders))
	for i, p := range ddnsProviders {
		if e := s.fetch(ctx, "connection/ddns/"+p.ID+"/", &p.Config); e != nil {
			p.Err = humanError(e)
		}
		_ = s.fetch(ctx, "connection/ddns/"+p.ID+"/status/", &p.Status)
		providers[i] = p
	}
	data["DDNS"] = providers
	s.show(w, r, "connection", data, nil)
}

var sipModes = map[string]bool{"disabled": true, "direct_media": true, "any_media": true}

func (s *Server) connectionConfig(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/connection") {
		return
	}
	sip := formStr(r, "sip_alg")
	if !sipModes[sip] {
		setFlash(w, "err:Mode SIP ALG inconnu.")
		http.Redirect(w, r, "/connection", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "connection/config/", map[string]any{
		"ping": formBool(r, "ping"), "wol": formBool(r, "wol"), "adblock": formBool(r, "adblock"),
		"allow_token_request": formBool(r, "allow_token_request"), "sip_alg": sip,
	}, "Réglages enregistrés.", "/connection")
}

// connectionRemote changes the remote access; turning it on exposes the box
// to the internet, so that needs the confirmation.
func (s *Server) connectionRemote(w http.ResponseWriter, r *http.Request) {
	on, port := formBool(r, "remote_access"), formInt(r, "remote_access_port")
	if on && !confirmed(w, r, "/connection") {
		return
	}
	if port < 1 || port > 65535 {
		setFlash(w, "err:Port invalide.")
		http.Redirect(w, r, "/connection", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "connection/config/", map[string]any{"remote_access": on, "remote_access_port": port},
		"Accès distant enregistré.", "/connection")
}

func (s *Server) connectionIPv6(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/connection") {
		return
	}
	var dels []map[string]string
	for i := range formInt(r, "n") {
		p := formStr(r, "prefix_"+strconv.Itoa(i))
		if p == "" {
			continue
		}
		dels = append(dels, map[string]string{"prefix": p, "next_hop": formStr(r, "next_hop_"+strconv.Itoa(i))})
	}
	s.change(w, r, http.MethodPut, "connection/ipv6/config/", map[string]any{
		"ipv6_enabled": formBool(r, "ipv6_enabled"), "ipv6_firewall": formBool(r, "ipv6_firewall"),
		"ipv6_prefix_firewall": formBool(r, "ipv6_prefix_firewall"), "delegations": dels,
	}, "IPv6 enregistré.", "/connection")
}

func (s *Server) connectionDDNS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("provider")
	known := false
	for _, p := range ddnsProviders {
		known = known || p.ID == id
	}
	if !known {
		http.NotFound(w, r)
		return
	}
	body := map[string]any{"enabled": formBool(r, "enabled"), "hostname": formStr(r, "hostname"), "user": formStr(r, "user")}
	if pw := r.PostFormValue("password"); pw != "" { // empty = keep the current one
		body["password"] = pw
	}
	s.change(w, r, http.MethodPut, "connection/ddns/"+id+"/", body, "DynDNS enregistré.", "/connection")
}
