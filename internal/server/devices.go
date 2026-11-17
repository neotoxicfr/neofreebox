package server

import (
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Devices: the hosts the box has seen on its LAN (lan/browser), renaming
// and Wake-on-LAN.

func init() {
	register(module{Slug: "devices", Title: "Appareils", Group: "Réseau", Order: 0, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /devices", s.devices)
		s.page(mux, "POST /devices/{id}/rename", s.renameDevice)
		s.page(mux, "POST /devices/{id}/wake", s.wakeDevice)
	}})
	ops.Register(
		ops.Op{Name: "lan_interfaces_list", Description: "List the LAN interfaces (pub = main LAN, wifiguest = guest Wi-Fi) and how many hosts each has seen.",
			Path: "lan/browser/interfaces/"},
		ops.Op{Name: "lan_hosts_list", Description: "List every host seen on a LAN interface: id, names, MAC (l2ident.id), vendor, type, active, IPv4/IPv6 addresses (l3connectivities), access point.",
			Path: "lan/browser/{interface}/", Params: []ops.Param{{Name: "interface", Description: `Interface name, usually "pub"`}}},
		ops.Op{Name: "lan_host_get", Description: "Get one LAN host.",
			Path: "lan/browser/{interface}/{id}/", Params: []ops.Param{{Name: "interface", Description: `Usually "pub"`}, {Name: "id", Description: `Host id, e.g. "ether-00:11:22:33:44:55"`}}},
		ops.Op{Name: "lan_host_update", Description: "Rename a LAN host or change its type.", Method: http.MethodPut,
			Path: "lan/browser/{interface}/{id}/", Params: []ops.Param{{Name: "interface", Description: `Usually "pub"`}, {Name: "id", Description: "Host id"}},
			Body: true, BodyHelp: `{"primary_name": "new name", "host_type": "workstation"}`},
		ops.Op{Name: "lan_wake_on_lan", Description: "Send a Wake-on-LAN packet to a MAC address on a LAN interface.", Method: http.MethodPost,
			Path: "lan/wol/{interface}/", Params: []ops.Param{{Name: "interface", Description: `Usually "pub"`}},
			Body: true, BodyHelp: `{"mac": "00:11:22:33:44:55", "password": ""}`},
	)
}

type lanHost struct {
	ID          string `json:"id"`
	PrimaryName string `json:"primary_name"`
	HostType    string `json:"host_type"`
	VendorName  string `json:"vendor_name"`
	Active      bool   `json:"active"`
	LastSeen    int64  `json:"last_time_reachable"`
	L2Ident     struct {
		ID string `json:"id"`
	} `json:"l2ident"`
	L3 []struct {
		Addr   string `json:"addr"`
		AF     string `json:"af"`
		Active bool   `json:"active"`
	} `json:"l3connectivities"`
	AccessPoint *struct {
		Type string `json:"type"`
	} `json:"access_point"`
}

// IPv4 is the host's current IPv4 address, if any.
func (h lanHost) IPv4() string {
	for _, a := range h.L3 {
		if a.AF == "ipv4" && a.Active {
			return a.Addr
		}
	}
	return ""
}

// Link says how the host is connected, from the access point it was seen on.
func (h lanHost) Link() string {
	if h.AccessPoint == nil {
		return ""
	}
	switch h.AccessPoint.Type {
	case "gateway", "repeater":
		return "Ethernet"
	case "wifi", "wifi_repeater":
		return "Wi-Fi"
	}
	return h.AccessPoint.Type
}

// maxRows is how many rows a long list shows before "Tout afficher".
const maxRows = 100

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	var hosts []lanHost
	err := s.fetch(r.Context(), "lan/browser/pub/", &hosts)
	all := r.URL.Query().Get("all") == "1"
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	shown := hosts[:0]
	active := 0
	for _, h := range hosts {
		if h.Active {
			active++
		}
		if (all || h.Active) && (q == "" || strings.Contains(strings.ToLower(h.PrimaryName+" "+h.L2Ident.ID+" "+h.IPv4()+" "+h.VendorName), q)) {
			shown = append(shown, h)
		}
	}
	sort.Slice(shown, func(i, j int) bool {
		if shown[i].Active != shown[j].Active {
			return shown[i].Active
		}
		return strings.ToLower(shown[i].PrimaryName) < strings.ToLower(shown[j].PrimaryName)
	})
	matched := len(shown)
	if r.URL.Query().Get("full") != "1" && len(shown) > maxRows {
		shown = shown[:maxRows]
	}
	s.show(w, r, "devices", map[string]any{"Hosts": shown, "Active": active, "Total": len(hosts), "All": all, "Q": q, "Matched": matched, "Capped": matched > len(shown)}, err)
}

func (s *Server) renameDevice(w http.ResponseWriter, r *http.Request) {
	id, name := r.PathValue("id"), formStr(r, "name")
	if name == "" {
		setFlash(w, "err:Le nom ne peut pas être vide.")
		http.Redirect(w, r, "/devices", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "lan/browser/pub/"+url.PathEscape(id)+"/",
		map[string]string{"id": id, "primary_name": name}, "Appareil renommé.", "/devices")
}

func (s *Server) wakeDevice(w http.ResponseWriter, r *http.Request) {
	mac := strings.TrimPrefix(r.PathValue("id"), "ether-")
	s.change(w, r, http.MethodPost, "lan/wol/pub/", map[string]string{"mac": mac, "password": ""},
		"Paquet de réveil envoyé.", "/devices")
}
