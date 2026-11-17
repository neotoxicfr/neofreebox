package server

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Wi-Fi MAC filter: the mode (off, whitelist, blacklist) and the list of
// addresses. A whitelist without your own device cuts you off.

func init() {
	register(module{Slug: "wifi-filter", Title: "Filtre MAC", Group: "Wi-Fi", Order: 30, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /wifi-filter", s.wifiFilter)
		s.page(mux, "POST /wifi-filter/state", s.wifiFilterState)
		s.page(mux, "POST /wifi-filter/entries", s.wifiFilterAdd)
		s.page(mux, "POST /wifi-filter/entries/{id}", s.wifiFilterEdit)
		s.page(mux, "POST /wifi-filter/entries/{id}/delete", s.wifiFilterDelete)
	}})
	entry := ops.Param{Name: "id", Description: `Filter id, which is the MAC address, e.g. "00:11:22:33:44:55"`}
	ops.Register(
		ops.Op{Name: "wifi_mac_filter_list", Description: "List the Wi-Fi MAC filter entries: id and mac (same value), type (whitelist or blacklist), comment, hostname, host. The mode itself is in wifi_config_get (mac_filter_state).",
			Path: "wifi/mac_filter/"},
		ops.Op{Name: "wifi_mac_filter_get", Description: "Get one MAC filter entry.",
			Path: "wifi/mac_filter/{id}", Params: []ops.Param{entry}},
		ops.Op{Name: "wifi_mac_filter_add", Description: "Add a MAC address to the Wi-Fi filter, as whitelist (allowed) or blacklist (rejected) entry. It only matters when the filter mode matches.",
			Method: http.MethodPost, Path: "wifi/mac_filter/", Body: true, Dangerous: true,
			BodyHelp: `{"mac": "00:11:22:33:44:55", "type": "blacklist", "comment": "old tablet"}`},
		ops.Op{Name: "wifi_mac_filter_update", Description: "Change the type or the comment of a MAC filter entry. Moving an entry out of the whitelist can cut that device.",
			Method: http.MethodPut, Path: "wifi/mac_filter/{id}", Params: []ops.Param{entry}, Body: true, Dangerous: true,
			BodyHelp: `{"type": "whitelist", "comment": "my phone"}`},
		ops.Op{Name: "wifi_mac_filter_delete", Description: "Delete a MAC filter entry. Removing a device from an active whitelist cuts it off.",
			Method: http.MethodDelete, Path: "wifi/mac_filter/{id}", Params: []ops.Param{entry}, Dangerous: true},
	)
}

type wifiFilterEntry struct {
	ID       string `json:"id"`
	MAC      string `json:"mac"`
	Type     string `json:"type"`
	Comment  string `json:"comment"`
	Hostname string `json:"hostname"`
}

func (e wifiFilterEntry) Allowed() bool { return e.Type == "whitelist" }

func (s *Server) wifiFilter(w http.ResponseWriter, r *http.Request) {
	var cfg wifiConfig
	var entries []wifiFilterEntry
	err := s.fetch(r.Context(), "wifi/config/", &cfg)
	if e := s.fetch(r.Context(), "wifi/mac_filter/", &entries); err == nil {
		err = e
	}
	white := 0
	for _, e := range entries {
		if e.Allowed() {
			white++
		}
	}
	entries, more := capList(entries, r.URL.Query().Get("all") == "1")
	s.show(w, r, "wifi-filter", map[string]any{"Cfg": cfg, "Entries": entries, "More": more, "Whitelisted": white, "Active": cfg.MACFilter != "" && cfg.MACFilter != "disabled"}, err)
}

// filterActive tells whether editing the list can already cut devices off.
func (s *Server) filterActive(r *http.Request) (bool, error) {
	var cfg wifiConfig
	if err := s.fetch(r.Context(), "wifi/config/", &cfg); err != nil {
		return false, err
	}
	return cfg.MACFilter != "" && cfg.MACFilter != "disabled", nil
}

func (s *Server) wifiFilterState(w http.ResponseWriter, r *http.Request) {
	const back = "/wifi-filter"
	state := formStr(r, "mac_filter_state")
	if state != "disabled" && state != "whitelist" && state != "blacklist" {
		setFlash(w, "err:Mode de filtre invalide.")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	if state != "disabled" {
		if !confirmed(w, r, back) {
			return
		}
		if state == "whitelist" {
			var entries []wifiFilterEntry
			if err := s.fetch(r.Context(), "wifi/mac_filter/", &entries); err != nil {
				setFlash(w, "err:Liste illisible : "+err.Error())
				http.Redirect(w, r, back, http.StatusSeeOther)
				return
			}
			white := false
			for _, e := range entries {
				white = white || e.Allowed()
			}
			if !white {
				setFlash(w, "err:La liste blanche est vide : tous les appareils, le tien compris, seraient refusés. Ajoute d'abord ton appareil.")
				http.Redirect(w, r, back, http.StatusSeeOther)
				return
			}
		}
	}
	s.change(w, r, http.MethodPut, "wifi/config/", map[string]string{"mac_filter_state": state}, "Filtre MAC enregistré.", back)
}

func (s *Server) wifiFilterAdd(w http.ResponseWriter, r *http.Request) {
	const back = "/wifi-filter"
	mac, typ := strings.ToUpper(formStr(r, "mac")), formStr(r, "type")
	if !macRe.MatchString(mac) || (typ != "whitelist" && typ != "blacklist") {
		setFlash(w, "err:Adresse MAC (00:11:22:33:44:55) ou type invalide.")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	if !s.filterConfirmed(w, r, back) {
		return
	}
	s.change(w, r, http.MethodPost, "wifi/mac_filter/", map[string]string{"mac": mac, "type": typ, "comment": formStr(r, "comment")}, "Adresse ajoutée au filtre.", back)
}

// filterConfirmed asks for the confirmation box only when the filter is on.
func (s *Server) filterConfirmed(w http.ResponseWriter, r *http.Request, back string) bool {
	active, err := s.filterActive(r)
	if err != nil {
		setFlash(w, "err:"+err.Error())
		http.Redirect(w, r, back, http.StatusSeeOther)
		return false
	}
	return !active || confirmed(w, r, back)
}

func (s *Server) wifiFilterEdit(w http.ResponseWriter, r *http.Request) {
	const back = "/wifi-filter"
	id, typ := r.PathValue("id"), formStr(r, "type")
	if !macRe.MatchString(id) || (typ != "whitelist" && typ != "blacklist") {
		setFlash(w, "err:Entrée ou type invalide.")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	if !s.filterConfirmed(w, r, back) {
		return
	}
	s.change(w, r, http.MethodPut, "wifi/mac_filter/"+url.PathEscape(id), map[string]string{"type": typ, "comment": formStr(r, "comment")}, "Entrée enregistrée.", back)
}

func (s *Server) wifiFilterDelete(w http.ResponseWriter, r *http.Request) {
	const back = "/wifi-filter"
	id := r.PathValue("id")
	if !macRe.MatchString(id) {
		setFlash(w, "err:Entrée invalide.")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	if !confirmed(w, r, back) {
		return
	}
	s.change(w, r, http.MethodDelete, "wifi/mac_filter/"+url.PathEscape(id), nil, "Entrée supprimée.", back)
}
