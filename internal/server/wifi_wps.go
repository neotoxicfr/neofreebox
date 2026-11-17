package server

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Wi-Fi WPS: push-button pairing sessions on a network.

func init() {
	register(module{Slug: "wifi-wps", Title: "WPS", Group: "Wi-Fi", Order: 35, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /wifi-wps", s.wifiWPS)
		s.page(mux, "POST /wifi-wps/config", s.wifiWPSConfig)
		s.page(mux, "POST /wifi-wps/start", s.wifiWPSStart)
		s.page(mux, "POST /wifi-wps/stop", s.wifiWPSStop)
		s.page(mux, "POST /wifi-wps/clear", s.wifiWPSClear)
	}})
	ops.Register(
		ops.Op{Name: "wifi_wps_config_get", Description: "Get the global WPS state: enabled is true if at least one network has WPS on.",
			Path: "wifi/wps/config/"},
		ops.Op{Name: "wifi_wps_config_set", Description: "Enable or disable WPS on every network. Enabling it lets anyone with physical access pair a device with the push-button method.",
			Method: http.MethodPut, Path: "wifi/wps/config/", Body: true, Dangerous: true, BodyHelp: `{"enabled": false}`},
		ops.Op{Name: "wifi_wps_sessions_list", Description: "List the WPS sessions: id, ssid, active, result (success, user_canceled, failed_timeout…), start_date, end_date, mac of the paired device.",
			Path: "wifi/wps/sessions/"},
		ops.Op{Name: "wifi_wps_sessions_clear", Description: "Clear the history of WPS sessions.",
			Method: http.MethodDelete, Path: "wifi/wps/sessions/"},
		ops.Op{Name: "wifi_wps_start", Description: "Open a WPS pairing session on a network (bssid from wifi_bss_list, with wps_enabled and WPA2 CCMP security). Returns the session id. A device can join without the key while it is open; only one session at a time.",
			Method: http.MethodPost, Path: "wifi/wps/start/", Body: true, Dangerous: true, BodyHelp: `{"bssid": "00:11:22:33:44:55"}`},
		ops.Op{Name: "wifi_wps_stop", Description: "Close an open WPS session.",
			Method: http.MethodPost, Path: "wifi/wps/stop/", Body: true, BodyHelp: `{"session_id": 1}`},
	)
}

type wifiWPSSession struct {
	ID     int    `json:"id"`
	SSID   string `json:"ssid"`
	Active bool   `json:"active"`
	Result string `json:"result"`
	Start  int64  `json:"start_date"`
	End    int64  `json:"end_date"`
	MAC    string `json:"mac"`
}

// ResultLabel is how the session ended, in French.
func (s wifiWPSSession) ResultLabel() string {
	if s.Active {
		return "en cours"
	}
	labels := map[string]string{
		"success": "appareil appairé", "user_canceled": "annulée", "self_canceled": "annulée (redémarrage du réseau)",
		"failed_timeout": "aucun appareil n'est venu", "failed_overlap": "une autre session était ouverte", "failed_unknown": "échec",
	}
	if l, ok := labels[s.Result]; ok {
		return l
	}
	return s.Result
}

// wifiWPSNet is a network that may take a WPS session.
type wifiWPSNet struct {
	BSSID, SSID, AP, Why string
	Ready                bool
}

func (s *Server) wifiWPS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var cfg struct {
		Enabled bool `json:"enabled"`
	}
	var sessions []wifiWPSSession
	err := s.fetch(ctx, "wifi/wps/config/", &cfg)
	if e := s.fetch(ctx, "wifi/wps/sessions/", &sessions); err == nil {
		err = e
	}
	var bsss []wifiBSS
	var aps []wifiAP
	s.soft(ctx, "wifi/bss/", &bsss)
	s.soft(ctx, "wifi/ap/", &aps)
	names := map[int]string{}
	for _, a := range aps {
		names[a.ID] = a.Name
	}
	var nets []wifiWPSNet
	for _, b := range bsss {
		n := wifiWPSNet{BSSID: b.ID, SSID: b.Config.SSID, AP: names[b.PhyID], Ready: true}
		switch {
		case !b.Config.WPS:
			n.Ready, n.Why = false, "WPS désactivé"
		case !b.Active():
			n.Ready, n.Why = false, "réseau "+b.StateLabel()
		case b.Config.HideSSID:
			n.Ready, n.Why = false, "nom masqué"
		case b.Config.Encryption != "wpa2_psk_ccmp" && b.Config.Encryption != "wpa2_psk_auto":
			n.Ready, n.Why = false, "sécurité incompatible (WPA2 requis)"
		}
		nets = append(nets, n)
	}
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].Start > sessions[j].Start })
	active := 0
	for _, x := range sessions {
		if x.Active {
			active = x.ID
		}
	}
	s.show(w, r, "wifi-wps", map[string]any{"Enabled": cfg.Enabled, "Sessions": sessions, "Nets": nets, "ActiveID": active}, err)
}

func (s *Server) wifiWPSConfig(w http.ResponseWriter, r *http.Request) {
	on := formBool(r, "enabled")
	if on && !confirmed(w, r, "/wifi-wps") {
		return
	}
	msg := "WPS désactivé."
	if on {
		msg = "WPS activé."
	}
	s.change(w, r, http.MethodPut, "wifi/wps/config/", map[string]bool{"enabled": on}, msg, "/wifi-wps")
}

func (s *Server) wifiWPSStart(w http.ResponseWriter, r *http.Request) {
	bssid := formStr(r, "bssid")
	if !macRe.MatchString(bssid) {
		setFlash(w, "err:Réseau invalide.")
		http.Redirect(w, r, "/wifi-wps", http.StatusSeeOther)
		return
	}
	if !confirmed(w, r, "/wifi-wps") {
		return
	}
	s.change(w, r, http.MethodPost, "wifi/wps/start/", map[string]string{"bssid": bssid}, "Session WPS ouverte : lance le WPS sur l'appareil.", "/wifi-wps")
}

func (s *Server) wifiWPSStop(w http.ResponseWriter, r *http.Request) {
	id := formInt(r, "session_id")
	if id <= 0 {
		setFlash(w, "err:Session invalide.")
		http.Redirect(w, r, "/wifi-wps", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPost, "wifi/wps/stop/", map[string]any{"session_id": id}, "Session WPS "+strconv.Itoa(id)+" fermée.", "/wifi-wps")
}

func (s *Server) wifiWPSClear(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodDelete, "wifi/wps/sessions/", nil, "Historique WPS effacé.", "/wifi-wps")
}
