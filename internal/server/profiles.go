package server

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Parental control: profiles, the devices attached to them (network
// control), their weekly access rules, an immediate pause, and the default
// filter mode.

const defaultProfileIcon = "/resources/images/profile/profile_04.png"

func init() {
	register(module{Slug: "profiles", Title: "Contrôle parental", Group: "Système", Order: 30, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /profiles", s.profiles)
		s.page(mux, "POST /profiles/add", s.addProfile)
		s.page(mux, "POST /profiles/default", s.setDefaultFilter)
		s.page(mux, "POST /profiles/{id}/rename", s.renameProfile)
		s.page(mux, "POST /profiles/{id}/delete", s.deleteProfile)
		s.page(mux, "POST /profiles/{id}/attach", s.attachDevice)
		s.page(mux, "POST /profiles/{id}/detach", s.detachDevice)
		s.page(mux, "POST /profiles/{id}/pause", s.pauseProfile)
		s.page(mux, "POST /profiles/{id}/resume", s.resumeProfile)
		s.page(mux, "POST /profiles/{id}/rules/add", s.addRule)
		s.page(mux, "POST /profiles/{id}/rules/{rule}/toggle", s.toggleRule)
		s.page(mux, "POST /profiles/{id}/rules/{rule}/delete", s.deleteRule)
	}})
	pid := ops.Param{Name: "profile_id", Description: "Profile id"}
	rid := ops.Param{Name: "rule_id", Description: "Rule id"}
	ruleBody := `{"name": "Night", "mode": "denied", "start_time": 75600, "end_time": 25200, "weekdays": [true,true,true,true,true,false,false,false], "enabled": true}`
	ops.Register(
		ops.Op{Name: "profile_list", Description: "List the parental-control profiles: id, name, icon url.", Path: "profile/"},
		ops.Op{Name: "profile_get", Description: "Get one profile.", Path: "profile/{id}/", Params: []ops.Param{{Name: "id", Description: "Profile id"}}},
		ops.Op{Name: "profile_add", Description: "Create a profile; returns its id.", Method: http.MethodPost,
			Path: "profile/", Body: true, BodyHelp: `{"name": "Kids", "url": "/resources/images/profile/profile_04.png"}`},
		ops.Op{Name: "profile_update", Description: "Rename a profile or change its icon.", Method: http.MethodPut,
			Path: "profile/{id}/", Params: []ops.Param{{Name: "id", Description: "Profile id"}}, Body: true, BodyHelp: `{"name": "Kids", "url": "/resources/images/profile/profile_02.png"}`},
		ops.Op{Name: "profile_delete", Description: "Delete a profile; its devices are no longer filtered by it.", Method: http.MethodDelete,
			Path: "profile/{id}/", Params: []ops.Param{{Name: "id", Description: "Profile id"}}, Dangerous: true},
		ops.Op{Name: "network_control_list", Description: "List the network control of every profile: current_mode (allowed, denied, webonly), rule_mode, override, override_mode, override_until, next_change, macs and hosts attached.",
			Path: "network_control/"},
		ops.Op{Name: "network_control_get", Description: "Get the network control of one profile (current mode, override, attached MAC addresses).",
			Path: "network_control/{profile_id}/", Params: []ops.Param{pid}},
		ops.Op{Name: "network_control_update", Description: "Update a profile's network control: attach devices (macs), or pause access now with override=true and override_mode=denied (override_until = unix time, 0 = until lifted). Send the whole object read with network_control_get.", Method: http.MethodPut,
			Path: "network_control/{profile_id}/", Params: []ops.Param{pid}, Dangerous: true, Body: true,
			BodyHelp: `{"override": true, "override_mode": "denied", "override_until": 1790000000, "macs": ["00:11:22:33:44:55"], "cdayranges": []}`},
		ops.Op{Name: "network_control_migrate_get", Description: "Tell whether the migration of the default filter mode to \"allowed\" was done (default_mode_migrated).", Path: "network_control/migrate"},
		ops.Op{Name: "network_control_migrate", Description: "Migrate the default filter mode to \"allowed\" if it was modified previously.", Method: http.MethodPost, Path: "network_control/migrate"},
		ops.Op{Name: "network_control_rule_list", Description: "List a profile's weekly access rules: id, name, mode, start_time and end_time (seconds since 00:00, multiples of 300), weekdays (7 booleans from Monday, the 8th is for custom day ranges), enabled.",
			Path: "network_control/{profile_id}/rules/", Params: []ops.Param{pid}},
		ops.Op{Name: "network_control_rule_get", Description: "Get one access rule of a profile.",
			Path: "network_control/{profile_id}/rules/{rule_id}/", Params: []ops.Param{pid, rid}},
		ops.Op{Name: "network_control_rule_add", Description: "Add an access rule to a profile. A denied or webonly rule cuts access during its hours.", Method: http.MethodPost,
			Path: "network_control/{profile_id}/rules/", Params: []ops.Param{pid}, Dangerous: true, Body: true, BodyHelp: ruleBody},
		ops.Op{Name: "network_control_rule_update", Description: "Update an access rule (name, mode, hours, days, enabled).", Method: http.MethodPut,
			Path: "network_control/{profile_id}/rules/{rule_id}/", Params: []ops.Param{pid, rid}, Dangerous: true, Body: true, BodyHelp: ruleBody},
		ops.Op{Name: "network_control_rule_delete", Description: "Delete an access rule of a profile.", Method: http.MethodDelete,
			Path: "network_control/{profile_id}/rules/{rule_id}/", Params: []ops.Param{pid, rid}, Dangerous: true},
		ops.Op{Name: "parental_config_get", Description: "Get the default filter mode (default_filter_mode: allowed, denied or webonly) applied to devices without a profile.", Path: "parental/config/"},
		ops.Op{Name: "parental_config_update", Description: "Set the default filter mode for devices without a profile. Anything but allowed cuts those devices off.", Method: http.MethodPut,
			Path: "parental/config/", Dangerous: true, Body: true, BodyHelp: `{"default_filter_mode": "allowed"}`},
	)
}

var filterModes = map[string]string{"allowed": "Autorisé", "denied": "Bloqué", "webonly": "Web seul"}

func modeLabel(m string) string {
	if l, ok := filterModes[m]; ok {
		return l
	}
	return m
}

type profileInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Icon string `json:"url"`
}

type networkControl struct {
	CurrentMode   string   `json:"current_mode"`
	RuleMode      string   `json:"rule_mode"`
	Override      bool     `json:"override"`
	OverrideMode  string   `json:"override_mode"`
	OverrideUntil int64    `json:"override_until"`
	NextChange    int64    `json:"next_change"`
	Macs          []string `json:"macs"`
}

type accessRule struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Mode      string `json:"mode"`
	StartTime int    `json:"start_time"`
	EndTime   int    `json:"end_time"`
	Weekdays  []bool `json:"weekdays"`
	Enabled   bool   `json:"enabled"`
}

// Days are listed from Monday; the 8th flag is for custom day ranges.
var dayNames = []string{"Lun", "Mar", "Mer", "Jeu", "Ven", "Sam", "Dim"}

// Days is the rule's days, readable ("Lun, Mar" or "Tous les jours").
func (r accessRule) Days() string {
	var d []string
	for i, n := range dayNames {
		if i < len(r.Weekdays) && r.Weekdays[i] {
			d = append(d, n)
		}
	}
	if len(d) == 7 {
		return "Tous les jours"
	}
	if len(r.Weekdays) > 7 && r.Weekdays[7] {
		d = append(d, "jours spéciaux")
	}
	return strings.Join(d, ", ")
}

func clock(sec int) string { return fmt.Sprintf("%02d:%02d", sec/3600, sec%3600/60) }

func (r accessRule) Hours() string     { return clock(r.StartTime) + " – " + clock(r.EndTime) }
func (r accessRule) ModeLabel() string { return modeLabel(r.Mode) }

type deviceRef struct{ MAC, Name string }

type profileView struct {
	profileInfo
	NC      networkControl
	Rules   []accessRule
	Devices []deviceRef
	Free    []deviceRef // LAN hosts not attached to this profile
	Err     string      // the box refused this profile's control data
}

func (p profileView) Mode() string     { return modeLabel(p.NC.CurrentMode) }
func (p profileView) Blocked() bool    { return p.NC.CurrentMode != "allowed" }
func (p profileView) Paused() bool     { return p.NC.Override && p.NC.OverrideMode != "allowed" }
func (p profileView) HasUntil() bool   { return p.NC.Override && p.NC.OverrideUntil > 0 }
func (p profileView) HasNext() bool    { return p.NC.NextChange > 0 }
func (p profileView) RuleMode() string { return modeLabel(p.NC.RuleMode) }

func upMAC(m string) string { return strings.ToUpper(strings.TrimSpace(m)) }

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Server) profiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var list []profileInfo
	err := s.fetch(ctx, "profile/", &list)
	var cfg struct {
		Mode string `json:"default_filter_mode"`
	}
	if err == nil {
		err = s.fetch(ctx, "parental/config/", &cfg)
	}
	var hosts []lanHost
	if err == nil {
		err = s.fetch(ctx, "lan/browser/pub/", &hosts)
	}
	names := map[string]string{}
	var known []deviceRef
	for _, h := range hosts {
		if h.L2Ident.ID == "" || h.PrimaryName == "" && h.VendorName == "" {
			continue
		}
		d := deviceRef{upMAC(h.L2Ident.ID), firstNonEmpty(h.PrimaryName, h.VendorName)}
		names[d.MAC] = d.Name
		known = append(known, d)
	}
	sort.Slice(known, func(i, j int) bool { return strings.ToLower(known[i].Name) < strings.ToLower(known[j].Name) })

	views := make([]profileView, 0, len(list))
	for _, p := range list {
		v := profileView{profileInfo: p}
		base := "network_control/" + strconv.Itoa(p.ID) + "/"
		if e := s.fetch(ctx, base, &v.NC); e != nil {
			v.Err = e.Error()
		} else if e := s.fetch(ctx, base+"rules/", &v.Rules); e != nil {
			v.Err = e.Error()
		}
		sort.Slice(v.Rules, func(i, j int) bool { return v.Rules[i].StartTime < v.Rules[j].StartTime })
		attached := map[string]bool{}
		for _, m := range v.NC.Macs {
			m = upMAC(m)
			attached[m] = true
			v.Devices = append(v.Devices, deviceRef{m, names[m]})
		}
		for _, d := range known {
			if !attached[d.MAC] {
				v.Free = append(v.Free, d)
			}
		}
		views = append(views, v)
	}
	s.show(w, r, "profiles", map[string]any{"Profiles": views, "Default": cfg.Mode, "DefaultLabel": modeLabel(cfg.Mode),
		"Modes": []string{"allowed", "denied", "webonly"}, "Labels": filterModes}, err)
}

func profileID(r *http.Request) (string, error) {
	n, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || n < 0 {
		return "", errors.New("identifiant de profil invalide")
	}
	return strconv.Itoa(n), nil
}

// refuse sends the user back to the page with an error message.
func refuse(w http.ResponseWriter, r *http.Request, msg string) {
	setFlash(w, "err:"+msg)
	http.Redirect(w, r, "/profiles", http.StatusSeeOther)
}

func (s *Server) addProfile(w http.ResponseWriter, r *http.Request) {
	name := formStr(r, "name")
	if name == "" {
		refuse(w, r, "Le nom du profil ne peut pas être vide.")
		return
	}
	s.change(w, r, http.MethodPost, "profile/", map[string]string{"name": name, "url": defaultProfileIcon}, "Profil créé.", "/profiles")
}

func (s *Server) renameProfile(w http.ResponseWriter, r *http.Request) {
	id, err := profileID(r)
	name := formStr(r, "name")
	if err != nil || name == "" {
		refuse(w, r, "Le nom du profil ne peut pas être vide.")
		return
	}
	// Keep the current icon: the box replaces the whole object.
	var cur profileInfo
	if err := s.fetch(r.Context(), "profile/"+id+"/", &cur); err != nil {
		refuse(w, r, err.Error())
		return
	}
	s.change(w, r, http.MethodPut, "profile/"+id+"/", map[string]string{"name": name, "url": firstNonEmpty(cur.Icon, defaultProfileIcon)}, "Profil renommé.", "/profiles")
}

func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id, err := profileID(r)
	if err != nil {
		refuse(w, r, err.Error())
		return
	}
	if !confirmed(w, r, "/profiles") {
		return
	}
	s.change(w, r, http.MethodDelete, "profile/"+id+"/", nil, "Profil supprimé.", "/profiles")
}

// putControl reads the profile's network control, lets edit change it, and
// writes the whole object back, as the box expects.
func (s *Server) putControl(w http.ResponseWriter, r *http.Request, edit func(nc map[string]any), done string) {
	id, err := profileID(r)
	if err != nil {
		refuse(w, r, err.Error())
		return
	}
	path := "network_control/" + id + "/"
	var nc map[string]any
	if err := s.fetch(r.Context(), path, &nc); err != nil {
		refuse(w, r, err.Error())
		return
	}
	delete(nc, "hosts") // read-only, derived from macs
	edit(nc)
	s.change(w, r, http.MethodPut, path, nc, done, "/profiles")
}

func macList(nc map[string]any) []string {
	out := []string{}
	if l, ok := nc["macs"].([]any); ok {
		for _, m := range l {
			if s, ok := m.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func (s *Server) attachDevice(w http.ResponseWriter, r *http.Request) {
	mac := upMAC(formStr(r, "mac"))
	if mac == "" {
		refuse(w, r, "Choisis un appareil.")
		return
	}
	s.putControl(w, r, func(nc map[string]any) {
		macs := macList(nc)
		for _, m := range macs {
			if upMAC(m) == mac {
				nc["macs"] = macs
				return
			}
		}
		nc["macs"] = append(macs, mac)
	}, "Appareil rattaché.")
}

func (s *Server) detachDevice(w http.ResponseWriter, r *http.Request) {
	mac := upMAC(formStr(r, "mac"))
	s.putControl(w, r, func(nc map[string]any) {
		keep := []string{}
		for _, m := range macList(nc) {
			if upMAC(m) != mac {
				keep = append(keep, m)
			}
		}
		nc["macs"] = keep
	}, "Appareil détaché.")
}

func (s *Server) pauseProfile(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/profiles") {
		return
	}
	minutes := formInt(r, "minutes")
	if minutes < 0 || minutes > 7*24*60 {
		refuse(w, r, "Durée invalide.")
		return
	}
	var until int64 // 0 = until resumed
	if minutes > 0 {
		until = time.Now().Add(time.Duration(minutes) * time.Minute).Unix()
	}
	s.putControl(w, r, func(nc map[string]any) {
		nc["override"], nc["override_mode"], nc["override_until"] = true, "denied", until
	}, "Accès coupé.")
}

func (s *Server) resumeProfile(w http.ResponseWriter, r *http.Request) {
	s.putControl(w, r, func(nc map[string]any) { nc["override"] = false }, "Accès rétabli selon le planning.")
}

// parseClock reads "HH:MM" as seconds since 00:00, in 5-minute steps.
func parseClock(v string) (int, bool) {
	h, m, ok := strings.Cut(strings.TrimSpace(v), ":")
	hh, e1 := strconv.Atoi(h)
	mm, e2 := strconv.Atoi(m)
	if !ok || e1 != nil || e2 != nil || hh < 0 || hh > 24 || mm < 0 || mm > 59 || mm%5 != 0 || hh == 24 && mm != 0 {
		return 0, false
	}
	return hh*3600 + mm*60, true
}

func (s *Server) addRule(w http.ResponseWriter, r *http.Request) {
	id, err := profileID(r)
	if err != nil {
		refuse(w, r, err.Error())
		return
	}
	mode := formStr(r, "mode")
	if _, ok := filterModes[mode]; !ok {
		refuse(w, r, "Mode inconnu.")
		return
	}
	if mode != "allowed" && !confirmed(w, r, "/profiles") {
		return
	}
	start, ok1 := parseClock(r.PostFormValue("start"))
	end, ok2 := parseClock(r.PostFormValue("end"))
	if !ok1 || !ok2 || start == end {
		refuse(w, r, "Heures invalides (pas de 5 minutes, début différent de la fin).")
		return
	}
	if end == 0 { // "00:00" as an end means midnight
		end = 86400
	}
	days := make([]bool, 8)
	picked := false
	for i := range 7 {
		days[i] = formBool(r, "day"+strconv.Itoa(i))
		picked = picked || days[i]
	}
	if !picked {
		refuse(w, r, "Choisis au moins un jour.")
		return
	}
	name := firstNonEmpty(formStr(r, "name"), modeLabel(mode)+" "+clock(start))
	s.change(w, r, http.MethodPost, "network_control/"+id+"/rules/", map[string]any{
		"name": name, "mode": mode, "start_time": start, "end_time": end, "weekdays": days, "enabled": true,
	}, "Règle ajoutée.", "/profiles")
}

func (s *Server) toggleRule(w http.ResponseWriter, r *http.Request) {
	id, err := profileID(r)
	rule, err2 := strconv.Atoi(r.PathValue("rule"))
	if err != nil || err2 != nil {
		refuse(w, r, "Identifiant invalide.")
		return
	}
	path := "network_control/" + id + "/rules/" + strconv.Itoa(rule) + "/"
	var cur map[string]any
	if err := s.fetch(r.Context(), path, &cur); err != nil {
		refuse(w, r, err.Error())
		return
	}
	enable := formBool(r, "enable")
	// Enabling a blocking rule cuts access.
	if m, _ := cur["mode"].(string); enable && m != "allowed" && !confirmed(w, r, "/profiles") {
		return
	}
	cur["enabled"] = enable
	s.change(w, r, http.MethodPut, path, cur, "Règle mise à jour.", "/profiles")
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := profileID(r)
	rule, err2 := strconv.Atoi(r.PathValue("rule"))
	if err != nil || err2 != nil {
		refuse(w, r, "Identifiant invalide.")
		return
	}
	s.change(w, r, http.MethodDelete, "network_control/"+id+"/rules/"+strconv.Itoa(rule)+"/", nil, "Règle supprimée.", "/profiles")
}

func (s *Server) setDefaultFilter(w http.ResponseWriter, r *http.Request) {
	mode := formStr(r, "mode")
	if _, ok := filterModes[mode]; !ok {
		refuse(w, r, "Mode inconnu.")
		return
	}
	if mode != "allowed" && !confirmed(w, r, "/profiles") {
		return
	}
	s.change(w, r, http.MethodPut, "parental/config/", map[string]string{"default_filter_mode": mode}, "Mode par défaut enregistré.", "/profiles")
}
