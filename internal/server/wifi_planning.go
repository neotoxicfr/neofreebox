package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Wi-Fi planning: the weekly slots when the Wi-Fi is on.

func init() {
	register(module{Slug: "wifi-planning", Title: "Planning", Group: "Wi-Fi", Order: 20, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /wifi-planning", s.wifiPlanning)
		s.page(mux, "POST /wifi-planning", s.wifiPlanningSave)
	}})
	ops.Register(
		ops.Op{Name: "wifi_planning_get", Description: "Get the Wi-Fi planning: use_planning, resolution (slots per day, usually 48) and mapping (7 x resolution values \"on\" or \"off\", Monday 00:00 first).",
			Path: "wifi/planning/"},
		ops.Op{Name: "wifi_planning_set", Description: "Change the Wi-Fi planning: enable it and/or send a new mapping. With the planning on, the Wi-Fi is cut during the \"off\" slots.",
			Method: http.MethodPut, Path: "wifi/planning/", Body: true, Dangerous: true,
			BodyHelp: `{"use_planning": true, "mapping": ["on", "on", "off", "..."]} (mapping optional; length 7 x resolution)`},
	)
}

var wifiDays = []string{"Lundi", "Mardi", "Mercredi", "Jeudi", "Vendredi", "Samedi", "Dimanche"}

type wifiPlan struct {
	Use        bool     `json:"use_planning"`
	Resolution int      `json:"resolution"`
	Mapping    []string `json:"mapping"`
}

type planSlot struct {
	Index int
	On    bool
	Label string
}

type planDay struct {
	Name  string
	Slots []planSlot
}

// slotTime formats the start of a slot of the day as hh:mm.
func slotTime(slot, resolution int) string {
	m := slot * 24 * 60 / resolution
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

func (s *Server) wifiPlanning(w http.ResponseWriter, r *http.Request) {
	var p wifiPlan
	err := s.fetch(r.Context(), "wifi/planning/", &p)
	var days []planDay
	var hours []string
	perHour := 1
	if err == nil && p.Resolution >= 24 && p.Resolution%24 == 0 && len(p.Mapping) == 7*p.Resolution {
		perHour = p.Resolution / 24
		for h := range 24 {
			hours = append(hours, strconv.Itoa(h)+" h")
		}
		for d, name := range wifiDays {
			day := planDay{Name: name}
			for i := range p.Resolution {
				idx := d*p.Resolution + i
				day.Slots = append(day.Slots, planSlot{Index: idx, On: p.Mapping[idx] == "on", Label: name + " " + slotTime(i, p.Resolution)})
			}
			days = append(days, day)
		}
	} else if err == nil {
		err = fmt.Errorf("planning inattendu (résolution %d, %d créneaux)", p.Resolution, len(p.Mapping))
	}
	s.show(w, r, "wifi-planning", map[string]any{"Plan": p, "Days": days, "Hours": hours, "PerHour": perHour, "SlotMin": 1440 / max(p.Resolution, 1)}, err)
}

func (s *Server) wifiPlanningSave(w http.ResponseWriter, r *http.Request) {
	const back = "/wifi-planning"
	var p wifiPlan
	if err := s.fetch(r.Context(), "wifi/planning/", &p); err != nil || p.Resolution < 1 {
		setFlash(w, "err:Impossible de lire le planning actuel.")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	n := 7 * p.Resolution
	on := make([]bool, n)
	switch formStr(r, "preset") {
	case "all":
		for i := range on {
			on[i] = true
		}
	case "night": // off from 00:00 to 06:00
		for i := range on {
			on[i] = i%p.Resolution >= p.Resolution/4
		}
	default:
		for _, v := range r.PostForm["slot"] {
			if i, err := strconv.Atoi(v); err == nil && i >= 0 && i < n {
				on[i] = true
			}
		}
	}
	mapping := make([]string, n)
	cut := false
	for i, v := range on {
		mapping[i] = "off"
		if v {
			mapping[i] = "on"
		} else {
			cut = true
		}
	}
	use := formBool(r, "use_planning")
	if use && cut && !confirmed(w, r, back) {
		return
	}
	s.change(w, r, http.MethodPut, "wifi/planning/", map[string]any{"use_planning": use, "mapping": mapping}, "Planning enregistré.", back)
}
