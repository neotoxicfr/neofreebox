package server

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Standby: the weekly planning that cuts Wi-Fi (or puts the box to sleep).

func init() {
	register(module{Slug: "standby", Title: "Veille", Group: "Système", Order: 20, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /standby", s.standby)
		s.page(mux, "POST /standby", s.saveStandby)
	}})
	ops.Register(
		ops.Op{Name: "standby_status_get", Description: "Get the standby planning status: use_planning, planning_mode, next_change (timestamp) and available_planning_modes (wifi_off, standby).",
			Path: "standby/status"},
		ops.Op{Name: "standby_config_get", Description: "Get the standby planning: use_planning, planning_mode, resolution (slots per day) and mapping (7 days from Monday 00:00 x resolution booleans, true = Wi-Fi off or box in standby).",
			Path: "standby/config/"},
		ops.Op{Name: "standby_config_update", Description: "Update the standby planning. Send the whole object, mapping included. An active planning cuts the Wi-Fi (or the box) in the slots set to true.", Method: http.MethodPut,
			Path: "standby/config/", Dangerous: true, Body: true,
			BodyHelp: `{"use_planning": true, "planning_mode": "wifi_off", "resolution": 48, "mapping": [false, false, "... 7*resolution booleans"]}`},
	)
}

type standbyConfig struct {
	UsePlanning  bool   `json:"use_planning"`
	PlanningMode string `json:"planning_mode"`
	Resolution   int    `json:"resolution"`
	Mapping      []bool `json:"mapping"`
}

type standbyStatus struct {
	UsePlanning    bool     `json:"use_planning"`
	PlanningMode   string   `json:"planning_mode"`
	NextChange     int64    `json:"next_change"`
	AvailableModes []string `json:"available_planning_modes"`
}

var modeLabels = map[string]string{"wifi_off": "Couper le Wi-Fi", "standby": "Mettre la box en veille"}

var standbyDays = []string{"Lundi", "Mardi", "Mercredi", "Jeudi", "Vendredi", "Samedi", "Dimanche"}

type modeChoice struct {
	Value, Label string
	Current      bool
}

type planDay struct {
	Name  string
	Hours []planHour
}

type planHour struct {
	Label string
	Slots []planSlot
}

type planSlot struct {
	Index int
	Name  string // for screen readers: "Lundi 08:30"
	On    bool
}

// PerHour is the number of slots in one hour.
func (c standbyConfig) PerHour() int { return c.Resolution / 24 }

// grid lays the mapping out as 7 days of 24 hours, each hour holding
// resolution/24 slots. It returns nil when the mapping does not fit.
func (c standbyConfig) grid() []planDay {
	if c.Resolution < 24 || c.Resolution%24 != 0 || len(c.Mapping) != 7*c.Resolution {
		return nil
	}
	per := c.Resolution / 24
	days := make([]planDay, 7)
	for d := range days {
		days[d].Name = standbyDays[d]
		for h := range 24 {
			hour := planHour{Label: pad2(h)}
			for k := range per {
				i := d*c.Resolution + h*per + k
				hour.Slots = append(hour.Slots, planSlot{Index: i, Name: standbyDays[d] + " " + pad2(h) + ":" + pad2(k*60/per), On: c.Mapping[i]})
			}
			days[d].Hours = append(days[d].Hours, hour)
		}
	}
	return days
}

func pad2(n int) string { return fmt.Sprintf("%02d", n) }

func (s *Server) standby(w http.ResponseWriter, r *http.Request) {
	var (
		st  standbyStatus
		cfg standbyConfig
	)
	err := s.fetch(r.Context(), "standby/status", &st)
	if err == nil {
		err = s.fetch(r.Context(), "standby/config/", &cfg)
	}
	var modes []modeChoice
	for _, m := range st.AvailableModes {
		label := modeLabels[m]
		if label == "" {
			label = m
		}
		modes = append(modes, modeChoice{m, label, m == cfg.PlanningMode})
	}
	s.show(w, r, "standby", map[string]any{"Status": st, "Cfg": cfg, "Modes": modes, "Grid": cfg.grid(), "ModeLabel": modeLabels[cfg.PlanningMode]}, err)
}

func (s *Server) saveStandby(w http.ResponseWriter, r *http.Request) {
	var (
		st  standbyStatus
		cfg standbyConfig
	)
	err := s.fetch(r.Context(), "standby/status", &st)
	if err == nil {
		err = s.fetch(r.Context(), "standby/config/", &cfg)
	}
	if err != nil || cfg.grid() == nil {
		setFlash(w, "err:Planning illisible : rien n'a été modifié.")
		http.Redirect(w, r, "/standby", http.StatusSeeOther)
		return
	}
	mode := formStr(r, "planning_mode")
	if !slices.Contains(st.AvailableModes, mode) {
		mode = cfg.PlanningMode
	}
	mapping := make([]bool, len(cfg.Mapping))
	cuts := false
	for _, v := range r.PostForm["slot"] {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < len(mapping) {
			mapping[n] = true
			cuts = true
		}
	}
	use := formBool(r, "use_planning")
	// A planning in effect cuts the Wi-Fi (or the box): confirmation needed.
	if use && cuts && !confirmed(w, r, "/standby") {
		return
	}
	s.change(w, r, http.MethodPut, "standby/config/",
		standbyConfig{UsePlanning: use, PlanningMode: mode, Resolution: cfg.Resolution, Mapping: mapping},
		"Planning de veille enregistré.", "/standby")
}
