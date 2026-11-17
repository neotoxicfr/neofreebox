package server

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// DHCP: IPv4 server settings, static and dynamic leases, DHCPv6.

func init() {
	register(module{Slug: "dhcp", Title: "DHCP", Group: "Réseau", Order: 10, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /dhcp", s.dhcp)
		s.page(mux, "POST /dhcp/config", s.saveDHCPConfig)
		s.page(mux, "POST /dhcp/enabled", s.setDHCPEnabled)
		s.page(mux, "POST /dhcp/static", s.addStaticLease)
		s.page(mux, "POST /dhcp/static/{id}/update", s.updateStaticLease)
		s.page(mux, "POST /dhcp/static/{id}/delete", s.deleteStaticLease)
		s.page(mux, "POST /dhcp/v6/enabled", s.setDHCPv6Enabled)
		s.page(mux, "POST /dhcp/v6/dns", s.setDHCPv6DNS)
	}})
	id := ops.Param{Name: "id", Description: `Lease id, the MAC address, e.g. "00:11:22:33:44:55"`}
	ops.Register(
		ops.Op{Name: "dhcp_config_get", Description: "Get the IPv4 DHCP server settings: enabled, sticky_assign, gateway, netmask, ip_range_start/end, dns list, boot_server/boot_file, options.",
			Path: "dhcp/config/"},
		ops.Op{Name: "dhcp_config_set", Description: "Change the IPv4 DHCP server settings (partial update). Disabling it leaves hosts without an address.", Method: http.MethodPut,
			Path: "dhcp/config/", Body: true, Dangerous: true,
			BodyHelp: `{"enabled": true, "sticky_assign": true, "ip_range_start": "192.168.1.100", "ip_range_end": "192.168.1.200", "dns": ["192.168.1.254"]}`},
		ops.Op{Name: "dhcp_static_lease_list", Description: "List the static DHCP leases: id (MAC), mac, ip, comment, hostname, host.",
			Path: "dhcp/static_lease/"},
		ops.Op{Name: "dhcp_static_lease_get", Description: "Get one static DHCP lease.",
			Path: "dhcp/static_lease/{id}", Params: []ops.Param{id}},
		ops.Op{Name: "dhcp_static_lease_add", Description: "Reserve an IPv4 address for a MAC address.", Method: http.MethodPost,
			Path: "dhcp/static_lease/", Body: true,
			BodyHelp: `{"mac": "00:11:22:33:44:55", "ip": "192.168.1.20", "comment": "printer"}`},
		ops.Op{Name: "dhcp_static_lease_update", Description: "Change the address or comment of a static DHCP lease.", Method: http.MethodPut,
			Path: "dhcp/static_lease/{id}", Params: []ops.Param{id}, Body: true,
			BodyHelp: `{"ip": "192.168.1.21", "comment": "printer"}`},
		ops.Op{Name: "dhcp_static_lease_delete", Description: "Delete a static DHCP lease.", Method: http.MethodDelete,
			Path: "dhcp/static_lease/{id}", Params: []ops.Param{id}},
		ops.Op{Name: "dhcp_dynamic_lease_list", Description: "List the active dynamic DHCP leases: mac, ip, hostname, lease_remaining (s), assign_time, refresh_time, is_static, host.",
			Path: "dhcp/dynamic_lease/"},
		ops.Op{Name: "dhcp_v6_config_get", Description: "Get the DHCPv6 server settings: enabled, use_custom_dns, dns.",
			Path: "dhcpv6/config/"},
		ops.Op{Name: "dhcp_v6_config_set", Description: "Change the DHCPv6 server settings (partial update). Disabling it can break IPv6 on hosts that rely on it.", Method: http.MethodPut,
			Path: "dhcpv6/config/", Body: true, Dangerous: true,
			BodyHelp: `{"enabled": true, "use_custom_dns": false}`},
	)
}

type dhcpConfig struct {
	Enabled              bool     `json:"enabled"`
	StickyAssign         bool     `json:"sticky_assign"`
	AlwaysBroadcast      bool     `json:"always_broadcast"`
	IgnoreOutOfRangeHint bool     `json:"ignore_out_of_range_hint"`
	Gateway              string   `json:"gateway"`
	Netmask              string   `json:"netmask"`
	IPRangeStart         string   `json:"ip_range_start"`
	IPRangeEnd           string   `json:"ip_range_end"`
	BootServer           string   `json:"boot_server"`
	BootFile             string   `json:"boot_file"`
	DNS                  []string `json:"dns"`
}

// DNSFields is the DNS list padded to the five inputs of the form.
func (c dhcpConfig) DNSFields() []dhcpDNSField {
	out := make([]dhcpDNSField, 5)
	for i := range out {
		out[i].N = i + 1
		if i < len(c.DNS) {
			out[i].Addr = c.DNS[i]
		}
	}
	return out
}

type dhcpDNSField struct {
	N    int
	Addr string
}

type dhcpHost struct {
	PrimaryName string `json:"primary_name"`
}

type dhcpStaticLease struct {
	ID       string   `json:"id"`
	MAC      string   `json:"mac"`
	IP       string   `json:"ip"`
	Comment  string   `json:"comment"`
	Hostname string   `json:"hostname"`
	Host     dhcpHost `json:"host"`
}

// Name is the best label the box knows for the leased host.
func (l dhcpStaticLease) Name() string { return dhcpFirst(l.Hostname, l.Host.PrimaryName) }

type dhcpDynamicLease struct {
	MAC            string   `json:"mac"`
	IP             string   `json:"ip"`
	Hostname       string   `json:"hostname"`
	LeaseRemaining int64    `json:"lease_remaining"`
	AssignTime     int64    `json:"assign_time"`
	RefreshTime    int64    `json:"refresh_time"`
	IsStatic       bool     `json:"is_static"`
	Host           dhcpHost `json:"host"`
}

func (l dhcpDynamicLease) Name() string { return dhcpFirst(l.Hostname, l.Host.PrimaryName) }

type dhcpv6Config struct {
	Enabled      bool     `json:"enabled"`
	UseCustomDNS bool     `json:"use_custom_dns"`
	DNS          []string `json:"dns"`
}

func dhcpFirst(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// dhcpIPv4 reports whether s is a valid IPv4 address.
func dhcpIPv4(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4()
}

func dhcpIPLess(a, b string) bool {
	x, e1 := netip.ParseAddr(a)
	y, e2 := netip.ParseAddr(b)
	if e1 != nil || e2 != nil {
		return a < b
	}
	return x.Less(y)
}

func (s *Server) dhcp(w http.ResponseWriter, r *http.Request) {
	var (
		cfg     dhcpConfig
		static  []dhcpStaticLease
		dynamic []dhcpDynamicLease
		v6      dhcpv6Config
	)
	var err error
	for _, f := range []struct {
		path string
		out  any
	}{{"dhcp/config/", &cfg}, {"dhcp/static_lease/", &static}, {"dhcp/dynamic_lease/", &dynamic}, {"dhcpv6/config/", &v6}} {
		if err = s.fetch(r.Context(), f.path, f.out); err != nil {
			break
		}
	}
	sort.Slice(static, func(i, j int) bool { return dhcpIPLess(static[i].IP, static[j].IP) })
	sort.Slice(dynamic, func(i, j int) bool { return dhcpIPLess(dynamic[i].IP, dynamic[j].IP) })
	total := len(dynamic)
	if r.URL.Query().Get("all") != "1" && total > maxRows {
		dynamic = dynamic[:maxRows]
	}
	// The box pads the DNS list with "::": not worth showing.
	v6.DNS = slices.DeleteFunc(v6.DNS, func(d string) bool { a, err := netip.ParseAddr(d); return err != nil || a.IsUnspecified() })
	s.show(w, r, "dhcp", map[string]any{"Config": cfg, "Static": static, "Dynamic": dynamic, "DynamicTotal": total, "V6": v6}, err)
}

func dhcpRefuse(w http.ResponseWriter, r *http.Request, back, msg string) {
	setFlash(w, "err:"+msg)
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (s *Server) saveDHCPConfig(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/dhcp") {
		return
	}
	start, end := formStr(r, "ip_range_start"), formStr(r, "ip_range_end")
	if !dhcpIPv4(start) || !dhcpIPv4(end) {
		dhcpRefuse(w, r, "/dhcp", "La plage doit être formée de deux adresses IPv4 valides.")
		return
	}
	dns := []string{}
	for i := 1; i <= 5; i++ {
		d := formStr(r, "dns"+strconv.Itoa(i))
		if d == "" {
			continue
		}
		if !dhcpIPv4(d) {
			dhcpRefuse(w, r, "/dhcp", "Serveur DNS invalide : "+d)
			return
		}
		dns = append(dns, d)
	}
	s.change(w, r, http.MethodPut, "dhcp/config/", map[string]any{
		"sticky_assign":            formBool(r, "sticky_assign"),
		"always_broadcast":         formBool(r, "always_broadcast"),
		"ignore_out_of_range_hint": formBool(r, "ignore_out_of_range_hint"),
		"ip_range_start":           start,
		"ip_range_end":             end,
		"boot_server":              formStr(r, "boot_server"),
		"boot_file":                formStr(r, "boot_file"),
		"dns":                      dns,
	}, "Réglages DHCP enregistrés.", "/dhcp")
}

// setDHCPEnabled turns the IPv4 server on or off; turning it off is dangerous.
func (s *Server) setDHCPEnabled(w http.ResponseWriter, r *http.Request) {
	on := formBool(r, "enabled")
	if !on && !confirmed(w, r, "/dhcp") {
		return
	}
	msg := "DHCP désactivé."
	if on {
		msg = "DHCP activé."
	}
	s.change(w, r, http.MethodPut, "dhcp/config/", map[string]any{"enabled": on}, msg, "/dhcp")
}

func (s *Server) setDHCPv6Enabled(w http.ResponseWriter, r *http.Request) {
	on := formBool(r, "enabled")
	if !on && !confirmed(w, r, "/dhcp") {
		return
	}
	msg := "DHCPv6 désactivé."
	if on {
		msg = "DHCPv6 activé."
	}
	s.change(w, r, http.MethodPut, "dhcpv6/config/", map[string]any{"enabled": on}, msg, "/dhcp")
}

func (s *Server) setDHCPv6DNS(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodPut, "dhcpv6/config/", map[string]any{"use_custom_dns": formBool(r, "use_custom_dns")},
		"Réglage DNS DHCPv6 enregistré.", "/dhcp")
}

func (s *Server) addStaticLease(w http.ResponseWriter, r *http.Request) {
	mac, err := net.ParseMAC(formStr(r, "mac"))
	ip := formStr(r, "ip")
	if err != nil || len(mac) != 6 || !dhcpIPv4(ip) {
		dhcpRefuse(w, r, "/dhcp", "Adresse MAC ou IPv4 invalide.")
		return
	}
	s.change(w, r, http.MethodPost, "dhcp/static_lease/", map[string]string{
		"mac": strings.ToUpper(mac.String()), "ip": ip, "comment": formStr(r, "comment"),
	}, "Bail statique ajouté.", "/dhcp")
}

func (s *Server) updateStaticLease(w http.ResponseWriter, r *http.Request) {
	ip := formStr(r, "ip")
	if !dhcpIPv4(ip) {
		dhcpRefuse(w, r, "/dhcp", "Adresse IPv4 invalide.")
		return
	}
	s.change(w, r, http.MethodPut, "dhcp/static_lease/"+url.PathEscape(r.PathValue("id")),
		map[string]string{"ip": ip, "comment": formStr(r, "comment")}, "Bail statique modifié.", "/dhcp")
}

func (s *Server) deleteStaticLease(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/dhcp") {
		return
	}
	s.change(w, r, http.MethodDelete, "dhcp/static_lease/"+url.PathEscape(r.PathValue("id")), nil, "Bail statique supprimé.", "/dhcp")
}
