package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Wi-Fi: global switch, radios (access points), networks (BSS), diagnostics,
// temporary disabling and reset. The other Wi-Fi pages (guest, planning, MAC
// filter, WPS, radar) live in wifi_*.go.

func init() {
	register(module{Slug: "wifi", Title: "Réseaux Wi-Fi", Group: "Wi-Fi", Order: 0, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /wifi", s.wifi)
		s.page(mux, "POST /wifi/enabled", s.wifiEnabled)
		s.page(mux, "POST /wifi/power", s.wifiPower)
		s.page(mux, "POST /wifi/steering", s.wifiSteering)
		s.page(mux, "POST /wifi/temp-disable", s.wifiTempDisable)
		s.page(mux, "POST /wifi/ap/{id}", s.wifiAPEdit)
		s.page(mux, "POST /wifi/ap/{id}/restart", s.wifiAPRestart)
		s.page(mux, "POST /wifi/bss/{id}", s.wifiBSSEdit)
		s.page(mux, "POST /wifi/diag/fix", s.wifiDiagFix)
		s.page(mux, "POST /wifi/reset", s.wifiReset)
	}})
	apID := ops.Param{Name: "id", Description: "Access point (radio) id, from wifi_ap_list: 0 is usually 2.4 GHz, 10 is 5 GHz"}
	bssID := ops.Param{Name: "id", Description: `BSS id, a MAC address such as "00:11:22:33:44:55" (from wifi_bss_list)`}
	ops.Register(
		ops.Op{Name: "wifi_config_get", Description: "Get the global Wi-Fi settings: enabled, power_saving, mac_filter_state (disabled, whitelist or blacklist).",
			Path: "wifi/config/"},
		ops.Op{Name: "wifi_config_set", Description: "Change the global Wi-Fi settings: switch Wi-Fi on or off, power saving, MAC filter mode. Switching Wi-Fi off or enabling a MAC filter can cut your own connection.",
			Method: http.MethodPut, Path: "wifi/config/", Body: true, Dangerous: true,
			BodyHelp: `{"enabled": true, "power_saving": false, "mac_filter_state": "disabled"} (every field optional)`},
		ops.Op{Name: "wifi_state_get", Description: "Get the global Wi-Fi state (enabled, disabled, disabled_planning) and the expected radio cards with a detected flag.",
			Path: "wifi/state/"},
		ops.Op{Name: "wifi_steering_get", Description: "Get the Wi-Fi steering level: 0 off, 1 devices are steered when they accept, 2 steered more aggressively.",
			Path: "wifi/steering/config/"},
		ops.Op{Name: "wifi_steering_set", Description: "Set the Wi-Fi steering level (0, 1 or 2).", Method: http.MethodPut,
			Path: "wifi/steering/config/", Body: true, BodyHelp: `{"steering_level": 1}`},
		ops.Op{Name: "wifi_ap_list", Description: "List the access points (one radio per band): id, name, config (band, channel_width, primary_channel, secondary_channel, dfs_enabled, ht/he/eht), status (state, effective channel) and capabilities.",
			Path: "wifi/ap/"},
		ops.Op{Name: "wifi_ap_get", Description: "Get one access point (radio) with its config, status and capabilities.",
			Path: "wifi/ap/{id}", Params: []ops.Param{apID}},
		ops.Op{Name: "wifi_ap_update", Description: "Change a radio: channel width, primary and secondary channel (0 = automatic), DFS. A wrong value can drop every device on that band, yours included.",
			Method: http.MethodPut, Path: "wifi/ap/{id}", Params: []ops.Param{apID}, Body: true, Dangerous: true,
			BodyHelp: `{"config": {"channel_width": "40", "primary_channel": 0, "secondary_channel": 0, "dfs_enabled": false}}`},
		ops.Op{Name: "wifi_ap_allowed_channels", Description: "List the channel combinations a radio accepts: band, channel_width, primary, secondary, need_dfs, dfs_cac_time, psc.",
			Path: "wifi/ap/{id}/allowed_channel_comb", Params: []ops.Param{apID}},
		ops.Op{Name: "wifi_ap_default", Description: "Get the default configuration of a radio (what a reset would apply).",
			Path: "wifi/ap/{id}/default", Params: []ops.Param{apID}},
		ops.Op{Name: "wifi_ap_restart", Description: "Restart a radio. Its devices are disconnected for a moment; the answer may never arrive if you are connected through it.",
			Method: http.MethodPost, Path: "wifi/ap/{id}/restart", Params: []ops.Param{apID}, Dangerous: true},
		ops.Op{Name: "wifi_ap_stations", Description: "List the stations (Wi-Fi clients) associated to a radio: mac, hostname, host, state, signal (dB), tx/rx rate and bytes, connection duration, bssid, flags.",
			Path: "wifi/ap/{id}/stations/", Params: []ops.Param{apID}},
		ops.Op{Name: "wifi_ap_station_get", Description: "Get one station associated to a radio.",
			Path: "wifi/ap/{id}/stations/{mac}", Params: []ops.Param{apID, {Name: "mac", Description: `Station MAC, e.g. "00:11:22:33:44:55"`}}},
		ops.Op{Name: "wifi_ap_survey_history", Description: "Get the channel survey history of a radio from a timestamp (milliseconds): busy_percent, tx_percent, rx_percent, rx_bss_percent.",
			Path: "wifi/ap/{id}/channel_survey_history/{timestamp}", Params: []ops.Param{apID, {Name: "timestamp", Type: "integer", Description: "Start timestamp in milliseconds (0 for everything)"}}},
		ops.Op{Name: "wifi_ap_neighbors", Description: "List the Wi-Fi networks of the neighbourhood seen by a radio: ssid, bssid, band, channel, channel_width, signal, capabilities.",
			Path: "wifi/ap/{id}/neighbors/", Params: []ops.Param{apID}},
		ops.Op{Name: "wifi_ap_channel_usage", Description: "List the channel usage seen by a radio: band, channel, noise_level, rx_busy_percent.",
			Path: "wifi/ap/{id}/channel_usage/", Params: []ops.Param{apID}},
		ops.Op{Name: "wifi_ap_scan", Description: "Launch a neighbour scan on a radio. The radio is unavailable during the scan; wait until its state leaves \"scanning\".",
			Method: http.MethodPost, Path: "wifi/ap/{id}/neighbors/scan", Params: []ops.Param{apID}, Dangerous: true},
		ops.Op{Name: "wifi_ap_diag_get", Description: "List the diagnostic items of a radio (severity minor or major, code).",
			Path: "wifi/ap/{id}/diag", Params: []ops.Param{apID}},
		ops.Op{Name: "wifi_ap_diag_fix", Description: "Apply the suggested fix of diagnostic codes on a radio (all, channel_width, channel_value). This changes the radio configuration.",
			Method: http.MethodPost, Path: "wifi/ap/{id}/diag", Params: []ops.Param{apID}, Body: true, BodyArray: true, Dangerous: true,
			BodyHelp: `a JSON array of codes: ["channel_width", "channel_value"]`},
		ops.Op{Name: "wifi_bss_list", Description: "List the Wi-Fi networks (BSS): id (MAC), phy_id, use_shared_params, config (enabled, ssid, encryption, hide_ssid, key, wps_enabled), status (state, sta_count) and the shared parameters. The key is returned in clear.",
			Path: "wifi/bss/"},
		ops.Op{Name: "wifi_bss_get", Description: "Get one Wi-Fi network (BSS), key included.",
			Path: "wifi/bss/{id}", Params: []ops.Param{bssID}},
		ops.Op{Name: "wifi_bss_update", Description: "Change a Wi-Fi network: ssid, encryption, key, hide_ssid, enabled. Changing the name, the key or the security disconnects every device, yours included.",
			Method: http.MethodPut, Path: "wifi/bss/{id}", Params: []ops.Param{bssID}, Body: true, Dangerous: true,
			BodyHelp: `{"config": {"ssid": "MyNetwork", "encryption": "wpa2_psk_ccmp", "key": "at least 8 characters", "hide_ssid": false, "enabled": true}}`},
		ops.Op{Name: "wifi_bss_default", Description: "Get the default configuration of a Wi-Fi network (what a reset would apply).",
			Path: "wifi/bss/{id}/default", Params: []ops.Param{bssID}},
		ops.Op{Name: "wifi_bss_diag_get", Description: "List the diagnostic items of a Wi-Fi network (network_disabled, network_security, network_visibility, all).",
			Path: "wifi/bss/{id}/diag", Params: []ops.Param{bssID}},
		ops.Op{Name: "wifi_bss_diag_fix", Description: "Apply the suggested fix of diagnostic codes on a Wi-Fi network. This changes its configuration (security, visibility).",
			Method: http.MethodPost, Path: "wifi/bss/{id}/diag", Params: []ops.Param{bssID}, Body: true, BodyArray: true, Dangerous: true,
			BodyHelp: `a JSON array of codes: ["network_security", "network_visibility"]`},
		ops.Op{Name: "wifi_bss_mlo_allowed", Description: "List the radio combinations a network can use for Multi-Link Operation (Wi-Fi 7), e.g. [[0, 10], [0]]. Not every box supports it.",
			Path: "wifi/bss/{id}/mlo/allowed_comb", Params: []ops.Param{bssID}},
		ops.Op{Name: "wifi_bss_mlo_get", Description: "Get the Multi-Link Operation partners of a network (empty = MLO off).",
			Path: "wifi/bss/{id}/mlo/config", Params: []ops.Param{bssID}},
		ops.Op{Name: "wifi_bss_mlo_set", Description: "Set the Multi-Link Operation partners of a network, from wifi_bss_mlo_allowed. Can briefly drop the devices on that network.",
			Method: http.MethodPut, Path: "wifi/bss/{id}/mlo/config/", Params: []ops.Param{bssID}, Body: true, Dangerous: true,
			BodyHelp: `{"partners": [0, 10]}`},
		ops.Op{Name: "wifi_defaults_get", Description: "Get the default configuration of every radio (aps) and network (bsss) in one call. The default keys are returned in clear.",
			Path: "wifi/default"},
		ops.Op{Name: "wifi_diag_get", Description: "Global Wi-Fi diagnostic: aps and bsss items with severity (minor or major) and code.",
			Path: "wifi/diag"},
		ops.Op{Name: "wifi_diag_fix", Description: "Apply the suggested fix of several diagnostic items at once. This changes radio and network configuration.",
			Method: http.MethodPost, Path: "wifi/diag", Body: true, Dangerous: true,
			BodyHelp: `{"aps": [{"ap_id": 0, "code": "channel_width"}], "bsss": [{"bssid": "00:11:22:33:44:55", "code": "network_security"}]}`},
		ops.Op{Name: "wifi_config_reset", Description: "Reset the whole Wi-Fi to the factory configuration: names, keys, channels. Every device must reconnect with the default key.",
			Method: http.MethodPost, Path: "wifi/config/reset/", Dangerous: true},
		ops.Op{Name: "wifi_temp_disable_get", Description: "Get the remaining seconds of a temporary Wi-Fi disabling (0 = not active).",
			Path: "wifi/temp_disable"},
		ops.Op{Name: "wifi_temp_disable_set", Description: "Disable Wi-Fi bands for a while, keeping one band active (useful to pair IoT devices). duration is in seconds, 0 stops it.",
			Method: http.MethodPost, Path: "wifi/temp_disable", Body: true, Dangerous: true,
			BodyHelp: `{"duration": 1200, "keep": "2d4g"} where keep is 2d4g, 5g or 6g`},
	)
}

// flexStr reads a JSON string or number as text: the box sends channel
// widths as strings but the documentation shows numbers.
type flexStr string

func (f *flexStr) UnmarshalJSON(b []byte) error {
	if s := strings.Trim(string(b), `"`); s != "null" {
		*f = flexStr(s)
	}
	return nil
}

var macRe = regexp.MustCompile(`^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$`)

// soft reads an optional endpoint: a failure only hides that section.
func (s *Server) soft(ctx context.Context, path string, out any) bool {
	if err := s.fetch(ctx, path, out); err != nil {
		s.log.Warn("wifi optional read", "path", path, "err", err)
		return false
	}
	return true
}

type wifiConfig struct {
	Enabled     bool   `json:"enabled"`
	PowerSaving bool   `json:"power_saving"`
	MACFilter   string `json:"mac_filter_state"`
}

// FilterLabel names the MAC filter mode.
func (c wifiConfig) FilterLabel() string { return wifiFilterLabel(c.MACFilter) }

func wifiFilterLabel(state string) string {
	switch state {
	case "disabled", "":
		return "désactivé"
	case "whitelist":
		return "liste blanche (seuls les appareils listés se connectent)"
	case "blacklist":
		return "liste noire (les appareils listés sont refusés)"
	}
	return state
}

type wifiState struct {
	State string `json:"state"`
	Phys  []struct {
		Band     string `json:"band"`
		Detected bool   `json:"detected"`
	} `json:"expected_phys"`
}

// Label says in French what the global state means.
func (st wifiState) Label() string {
	switch st.State {
	case "enabled":
		return "activé"
	case "disabled":
		return "désactivé"
	case "disabled_planning":
		return "désactivé par le planning"
	}
	return st.State
}

func (st wifiState) On() bool { return st.State == "enabled" }

type wifiAP struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Config struct {
		Band         string  `json:"band"`
		ChannelWidth flexStr `json:"channel_width"`
		Primary      int     `json:"primary_channel"`
		Secondary    int     `json:"secondary_channel"`
		DFS          bool    `json:"dfs_enabled"`
	} `json:"config"`
	Status struct {
		State        string  `json:"state"`
		ChannelWidth flexStr `json:"channel_width"`
		Primary      int     `json:"primary_channel"`
		Secondary    int     `json:"secondary_channel"`
		CAC          int     `json:"dfs_cac_remaining_time"`
	} `json:"status"`
}

// StateLabel is the radio state in French.
func (a wifiAP) StateLabel() string {
	labels := map[string]string{
		"scanning": "analyse des canaux", "no_param": "non configuré", "bad_param": "configuration invalide",
		"disabled": "désactivé", "disabled_planning": "coupé par le planning", "disabled_power_saving": "coupé (économie d'énergie)",
		"disabled_temp": "coupé temporairement", "no_active_bss": "aucun réseau actif", "starting": "démarrage", "stopping": "arrêt",
		"acs": "choix du canal", "ht_scan": "analyse des voisins", "dfs": "attente des radars (DFS)", "active": "en marche", "failed": "en échec",
	}
	if l, ok := labels[a.Status.State]; ok {
		return l
	}
	return a.Status.State
}

func (a wifiAP) Active() bool { return a.Status.State == "active" }

// Badge is the badge modifier: green when running, red on failure, orange
// when the configuration is wrong, neutral otherwise (off, planning, starting).
func (a wifiAP) Badge() string { return wifiBadge(a.Status.State, "active") }

func wifiBadge(state, active string) string {
	switch state {
	case active:
		return "on"
	case "failed":
		return "off"
	case "no_param", "bad_param":
		return "warn"
	}
	return ""
}

// Channel is the channel in use, with its secondary channel when there is one.
func (a wifiAP) Channel() string {
	if a.Status.Primary == 0 {
		return "—"
	}
	if a.Status.Secondary != 0 {
		return strconv.Itoa(a.Status.Primary) + " + " + strconv.Itoa(a.Status.Secondary)
	}
	return strconv.Itoa(a.Status.Primary)
}

// Wanted tells what the configuration asks for.
func (a wifiAP) Wanted() string {
	if a.Config.Primary == 0 {
		return "automatique"
	}
	return "canal " + strconv.Itoa(a.Config.Primary)
}

var wifiWidths = []string{"20", "40", "80", "160", "320"}

// WidthOptions lists the channel widths, the current one selected.
func (a wifiAP) WidthOptions() []wifiOption {
	var out []wifiOption
	for _, w := range wifiWidths {
		out = append(out, wifiOption{Value: w, Label: w + " MHz", Selected: string(a.Config.ChannelWidth) == w})
	}
	return out
}

type wifiOption struct {
	Value, Label string
	Selected     bool
}

type wifiBSS struct {
	ID        string `json:"id"`
	PhyID     int    `json:"phy_id"`
	UseShared bool   `json:"use_shared_params"`
	Config    struct {
		Enabled    bool   `json:"enabled"`
		SSID       string `json:"ssid"`
		Encryption string `json:"encryption"`
		HideSSID   bool   `json:"hide_ssid"`
		Key        string `json:"key"`
		WPS        bool   `json:"wps_enabled"`
	} `json:"config"`
	Status struct {
		State      string `json:"state"`
		Stations   int    `json:"sta_count"`
		Authorized int    `json:"authorized_sta_count"`
	} `json:"status"`
	Band string `json:"-"` // band of its radio, filled by the page
}

var wifiEncryptions = []wifiOption{
	{Value: "wpa3_psk_ccmp", Label: "WPA3 seul"},
	{Value: "wpa23_psk_ccmp", Label: "WPA2 + WPA3 (transition)"},
	{Value: "wpa23_psk_ccmp_mrsno", Label: "WPA2 + WPA3 (compatibilité)"},
	{Value: "wpa2_psk_ccmp", Label: "WPA2 (AES)"},
	{Value: "wpa2_psk_auto", Label: "WPA2 (AES + TKIP, déconseillé)"},
	{Value: "wpa2_psk_tkip", Label: "WPA2 (TKIP, déconseillé)"},
	{Value: "wpa12_psk_auto", Label: "WPA1 + WPA2 (déconseillé)"},
	{Value: "wpa_psk_auto", Label: "WPA1 (déconseillé)"},
	{Value: "wpa_psk_ccmp", Label: "WPA1 AES (déconseillé)"},
	{Value: "wpa_psk_tkip", Label: "WPA1 TKIP (déconseillé)"},
	{Value: "wep", Label: "WEP (déconseillé)"},
}

func wifiEncryptionLabel(enc string) string {
	for _, o := range wifiEncryptions {
		if o.Value == enc {
			return o.Label
		}
	}
	return enc
}

func wifiEncryptionKnown(enc string) bool {
	for _, o := range wifiEncryptions {
		if o.Value == enc {
			return true
		}
	}
	return false
}

// EncLabel is the security mode in French.
func (b wifiBSS) EncLabel() string { return wifiEncryptionLabel(b.Config.Encryption) }

// EncOptions lists the security modes, the current one selected.
func (b wifiBSS) EncOptions() []wifiOption {
	out := make([]wifiOption, len(wifiEncryptions))
	for i, o := range wifiEncryptions {
		o.Selected = o.Value == b.Config.Encryption
		out[i] = o
	}
	return out
}

// StateLabel is the network state in French.
func (b wifiBSS) StateLabel() string {
	labels := map[string]string{
		"phy_stopped": "radio arrêtée", "no_param": "non configuré", "bad_param": "configuration invalide", "disabled": "désactivé",
		"temp_disabled": "coupé temporairement", "starting": "démarrage", "active": "activé", "failed": "en échec",
	}
	if l, ok := labels[b.Status.State]; ok {
		return l
	}
	return b.Status.State
}

func (b wifiBSS) Active() bool { return b.Status.State == "active" }

// Badge is the badge modifier of the network state.
func (b wifiBSS) Badge() string { return wifiBadge(b.Status.State, "active") }

// KeyHidden is true when the box masks the key (not enough rights).
func (b wifiBSS) KeyHidden() bool { return b.Config.Key == "" || strings.Trim(b.Config.Key, "*") == "" }

type wifiDiagItem struct {
	Severity string `json:"severity"`
	APID     *int   `json:"ap_id"`
	BSSID    string `json:"bssid"`
	Code     string `json:"code"`
}

// Target names the radio or network the item is about.
func (d wifiDiagItem) Target() string {
	if d.APID != nil {
		return "Radio " + strconv.Itoa(*d.APID)
	}
	return "Réseau " + d.BSSID
}

// Ref is the form value that identifies the target: "ap:0" or "bss:<mac>".
func (d wifiDiagItem) Ref() string {
	if d.APID != nil {
		return "ap:" + strconv.Itoa(*d.APID)
	}
	return "bss:" + d.BSSID
}

func (d wifiDiagItem) Major() bool { return d.Severity == "major" }

// Label explains the problem in French.
func (d wifiDiagItem) Label() string {
	labels := map[string]string{
		"all": "réglages à remettre par défaut", "network_disabled": "réseau désactivé", "network_security": "sécurité faible ou dépassée",
		"network_visibility": "visibilité du nom à revoir", "channel_width": "largeur de canal sous-optimale", "channel_value": "canal sous-optimal",
	}
	if l, ok := labels[d.Code]; ok {
		return l
	}
	return d.Code
}

type wifiDiag struct {
	APs  []wifiDiagItem `json:"aps"`
	BSSs []wifiDiagItem `json:"bsss"`
}

func (d wifiDiag) Items() []wifiDiagItem {
	return append(append([]wifiDiagItem{}, d.APs...), d.BSSs...)
}

func (s *Server) wifi(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var cfg wifiConfig
	var aps []wifiAP
	var bsss []wifiBSS
	err := s.fetch(ctx, "wifi/config/", &cfg)
	if e := s.fetch(ctx, "wifi/ap/", &aps); err == nil {
		err = e
	}
	if e := s.fetch(ctx, "wifi/bss/", &bsss); err == nil {
		err = e
	}
	var state wifiState
	s.soft(ctx, "wifi/state/", &state)
	var steering struct {
		Level int `json:"steering_level"`
	}
	hasSteering := s.soft(ctx, "wifi/steering/config/", &steering)
	var temp struct {
		Remaining int64 `json:"remaining"`
	}
	s.soft(ctx, "wifi/temp_disable", &temp)
	var diag wifiDiag
	s.soft(ctx, "wifi/diag", &diag)

	bands := map[int]string{}
	stations := map[int]int{}
	for _, a := range aps {
		bands[a.ID] = a.Config.Band
	}
	for i := range bsss {
		bsss[i].Band = bands[bsss[i].PhyID]
		stations[bsss[i].PhyID] += bsss[i].Status.Stations
	}
	sort.SliceStable(bsss, func(i, j int) bool { return bsss[i].PhyID < bsss[j].PhyID })
	s.show(w, r, "wifi", map[string]any{"Cfg": cfg, "State": state, "APs": aps, "BSSs": bsss, "Diag": diag.Items(),
		"Steering": steering.Level, "HasSteering": hasSteering, "TempRemaining": temp.Remaining, "Stations": stations}, err)
}

func (s *Server) wifiEnabled(w http.ResponseWriter, r *http.Request) {
	on := formBool(r, "enabled")
	if !on && !confirmed(w, r, "/wifi") {
		return
	}
	msg := "Wi-Fi activé."
	if !on {
		msg = "Wi-Fi coupé."
	}
	s.change(w, r, http.MethodPut, "wifi/config/", map[string]any{"enabled": on}, msg, "/wifi")
}

func (s *Server) wifiPower(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodPut, "wifi/config/", map[string]any{"power_saving": formBool(r, "power_saving")}, "Économie d'énergie enregistrée.", "/wifi")
}

func (s *Server) wifiSteering(w http.ResponseWriter, r *http.Request) {
	lvl := formInt(r, "steering_level")
	if lvl < 0 || lvl > 2 {
		setFlash(w, "err:Niveau de répartition invalide.")
		http.Redirect(w, r, "/wifi", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "wifi/steering/config/", map[string]any{"steering_level": lvl}, "Répartition des appareils enregistrée.", "/wifi")
}

func (s *Server) wifiTempDisable(w http.ResponseWriter, r *http.Request) {
	if formStr(r, "stop") == "1" {
		s.change(w, r, http.MethodPost, "wifi/temp_disable", map[string]any{"duration": 0}, "Coupure temporaire arrêtée.", "/wifi")
		return
	}
	if !confirmed(w, r, "/wifi") {
		return
	}
	minutes, keep := formInt(r, "minutes"), formStr(r, "keep")
	if minutes < 1 || minutes > 24*60 || (keep != "2d4g" && keep != "5g" && keep != "6g") {
		setFlash(w, "err:Durée (1 à 1440 minutes) ou bande à garder invalide.")
		http.Redirect(w, r, "/wifi", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPost, "wifi/temp_disable", map[string]any{"duration": minutes * 60, "keep": keep},
		"Les autres bandes sont coupées pour "+strconv.Itoa(minutes)+" min.", "/wifi")
}

// wifiID reads a numeric path value, or redirects back with an error.
func wifiID(w http.ResponseWriter, r *http.Request, name, back string) (int, bool) {
	n, err := strconv.Atoi(r.PathValue(name))
	if err != nil || n < 0 {
		setFlash(w, "err:Identifiant invalide.")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return 0, false
	}
	return n, true
}

func (s *Server) wifiAPEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := wifiID(w, r, "id", "/wifi")
	if !ok || !confirmed(w, r, "/wifi") {
		return
	}
	width := formStr(r, "channel_width")
	valid := false
	for _, v := range wifiWidths {
		valid = valid || v == width
	}
	primary, secondary := formInt(r, "primary_channel"), formInt(r, "secondary_channel")
	if !valid || primary < 0 || secondary < 0 || primary > 233 || secondary > 233 {
		setFlash(w, "err:Largeur ou canal invalide.")
		http.Redirect(w, r, "/wifi", http.StatusSeeOther)
		return
	}
	body := map[string]any{"config": map[string]any{"channel_width": width, "primary_channel": primary,
		"secondary_channel": secondary, "dfs_enabled": formBool(r, "dfs_enabled")}}
	s.change(w, r, http.MethodPut, "wifi/ap/"+strconv.Itoa(id), body, "Radio enregistrée.", "/wifi")
}

func (s *Server) wifiAPRestart(w http.ResponseWriter, r *http.Request) {
	id, ok := wifiID(w, r, "id", "/wifi")
	if !ok || !confirmed(w, r, "/wifi") {
		return
	}
	s.change(w, r, http.MethodPost, "wifi/ap/"+strconv.Itoa(id)+"/restart", nil, "Radio redémarrée.", "/wifi")
}

func (s *Server) wifiBSSEdit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !macRe.MatchString(id) {
		setFlash(w, "err:Identifiant de réseau invalide.")
		http.Redirect(w, r, "/wifi", http.StatusSeeOther)
		return
	}
	if !confirmed(w, r, "/wifi") {
		return
	}
	ssid, enc, key := formStr(r, "ssid"), formStr(r, "encryption"), r.PostFormValue("key")
	switch {
	case ssid == "" || len(ssid) > 32:
		setFlash(w, "err:Le nom du réseau fait de 1 à 32 octets.")
	case !wifiEncryptionKnown(enc):
		setFlash(w, "err:Mode de sécurité inconnu.")
	case key != "" && enc != "wep" && (len(key) < 8 || len(key) > 63):
		setFlash(w, "err:La clé fait de 8 à 63 caractères.")
	default:
		cfg := map[string]any{"ssid": ssid, "encryption": enc, "hide_ssid": formBool(r, "hide_ssid"), "enabled": formBool(r, "enabled")}
		if key != "" { // empty = keep the current key
			cfg["key"] = key
		}
		s.change(w, r, http.MethodPut, "wifi/bss/"+url.PathEscape(id), map[string]any{"config": cfg}, "Réseau enregistré.", "/wifi")
		return
	}
	http.Redirect(w, r, "/wifi", http.StatusSeeOther)
}

func (s *Server) wifiDiagFix(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/wifi") {
		return
	}
	kind, target, _ := strings.Cut(formStr(r, "target"), ":")
	code := formStr(r, "code")
	body := map[string]any{}
	switch {
	case code == "":
	case kind == "ap":
		if n, err := strconv.Atoi(target); err == nil && n >= 0 {
			body["aps"] = []map[string]any{{"ap_id": n, "code": code}}
		}
	case kind == "bss" && macRe.MatchString(target):
		body["bsss"] = []map[string]any{{"bssid": target, "code": code}}
	}
	if len(body) == 0 {
		setFlash(w, "err:Élément de diagnostic invalide.")
		http.Redirect(w, r, "/wifi", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPost, "wifi/diag", body, "Correctif appliqué.", "/wifi")
}

func (s *Server) wifiReset(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/wifi") {
		return
	}
	s.change(w, r, http.MethodPost, "wifi/config/reset/", nil, "Wi-Fi remis par défaut.", "/wifi")
}
