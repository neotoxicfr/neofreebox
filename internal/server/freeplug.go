package server

import (
	"net/http"
	"net/url"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Freeplugs: the powerline (CPL) networks the box sees, with their link
// rates, and the reset of one adapter.

func init() {
	register(module{Slug: "freeplug", Title: "Freeplugs", Group: "Supervision", Order: 20, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /freeplug", s.freeplugs)
		s.page(mux, "POST /freeplug/{id}/reset", s.resetFreeplug)
	}})
	ops.Register(
		ops.Op{Name: "freeplug_networks_list", Path: "freeplug/",
			Description: "List the powerline networks (id, members[]). A member has id (MAC), net_role (sta, pco, cco), local, model, eth_port_status, eth_speed, eth_full_duplex, has_network, inactive (s), rx_rate and tx_rate (Mb/s, -1 if unknown)."},
		ops.Op{Name: "freeplug_get", Path: "freeplug/{id}/", Description: "Get one Freeplug (same fields as the members of freeplug_networks_list).",
			Params: []ops.Param{{Name: "id", Description: `Freeplug MAC address, e.g. "00:11:22:33:44:55"`}}},
		ops.Op{Name: "freeplug_reset", Method: http.MethodPost, Path: "freeplug/{id}/reset/", Dangerous: true,
			Description: "Reset a Freeplug: its link drops while it restarts, so everything behind it loses the network for a moment.",
			Params:      []ops.Param{{Name: "id", Description: `Freeplug MAC address, e.g. "00:11:22:33:44:55"`}}},
	)
}

type freeplug struct {
	ID            string `json:"id"`
	Local         bool   `json:"local"`
	NetRole       string `json:"net_role"`
	Model         string `json:"model"`
	EthPortStatus string `json:"eth_port_status"`
	EthFullDuplex bool   `json:"eth_full_duplex"`
	HasNetwork    bool   `json:"has_network"`
	EthSpeed      int    `json:"eth_speed"`
	Inactive      int64  `json:"inactive"`
	RxRate        int    `json:"rx_rate"`
	TxRate        int    `json:"tx_rate"`
}

// Role is the network role in words.
func (f freeplug) Role() string {
	switch f.NetRole {
	case "cco":
		return "Coordinateur central"
	case "pco":
		return "Coordinateur proxy"
	case "sta":
		return "Station"
	}
	return f.NetRole
}

// Port is the state of the Ethernet port.
func (f freeplug) Port() string {
	switch f.EthPortStatus {
	case "up":
		return "Actif"
	case "down":
		return "Inactif"
	}
	return "Inconnu"
}

type freeplugNetwork struct {
	ID      string     `json:"id"`
	Members []freeplug `json:"members"`
}

func (s *Server) freeplugs(w http.ResponseWriter, r *http.Request) {
	var nets []freeplugNetwork
	err := s.fetch(r.Context(), "freeplug/", &nets)
	s.show(w, r, "freeplug", map[string]any{"Networks": nets}, err)
}

func (s *Server) resetFreeplug(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/freeplug") {
		return
	}
	s.change(w, r, http.MethodPost, "freeplug/"+url.PathEscape(r.PathValue("id"))+"/reset/", nil,
		"Freeplug réinitialisé.", "/freeplug")
}
