package server

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Display: the LCD screen and LEDs (lcd/config, ledstrip). Optional settings
// only exist on some models: the box leaves them out, and so does the page.

func init() {
	register(module{Slug: "display", Title: "Écran et voyants", Group: "Système", Order: 10, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /display", s.display)
		s.page(mux, "POST /display", s.saveDisplay)
		s.page(mux, "POST /display/ledstrip-planning", s.ledstripPlanning)
	}})
	ops.Register(
		ops.Op{Name: "lcd_config_get", Description: "Get the LCD and LED settings: brightness (0-100), orientation, orientation_forced, hide_wifi_key, hide_status_led, screensaver, led_strip_* (optional, model dependent).",
			Path: "lcd/config/"},
		ops.Op{Name: "lcd_config_update", Description: "Update the LCD and LED settings; send only the fields to change.", Method: http.MethodPut,
			Path: "lcd/config/", Body: true, BodyHelp: `{"brightness": 50, "orientation_forced": false, "hide_wifi_key": true, "hide_status_led": false}`},
		ops.Op{Name: "ledstrip_status_get", Description: "Get the LED strip planning status (use_planning, next_change). Only on boxes with a LED strip.",
			Path: "ledstrip/status"},
		ops.Op{Name: "ledstrip_planning_get", Description: "Get the LED strip planning: use_planning, planning_mode, resolution and mapping (7 days x resolution slots, true = LED strip off).",
			Path: "ledstrip/planning/"},
		ops.Op{Name: "ledstrip_planning_update", Description: "Update the LED strip planning. Send the whole object, mapping included.", Method: http.MethodPut,
			Path: "ledstrip/planning/", Body: true, BodyHelp: `{"use_planning": true, "planning_mode": "ledstrip_off", "resolution": 48, "mapping": [false, false, "... 7*resolution booleans"]}`},
	)
}

type lcdConfig struct {
	Brightness       int      `json:"brightness"`
	Orientation      int      `json:"orientation"`
	OrientationForce bool     `json:"orientation_forced"`
	HideWifiKey      bool     `json:"hide_wifi_key"`
	HideStatusLED    bool     `json:"hide_status_led"`
	Screensaver      string   `json:"screensaver"`
	StripEnabled     bool     `json:"led_strip_enabled"`
	StripBrightness  int      `json:"led_strip_brightness"`
	StripAnimation   string   `json:"led_strip_animation"`
	StripAnimations  []string `json:"available_led_strip_animations"`
	present          map[string]bool
}

// UnmarshalJSON also records which fields the box sent: a zero value and a
// missing field differ, and the page only shows what this model has.
func (c *lcdConfig) UnmarshalJSON(b []byte) error {
	type plain lcdConfig
	if err := json.Unmarshal(b, (*plain)(c)); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	c.present = make(map[string]bool, len(raw))
	for k := range raw {
		c.present[k] = true
	}
	return nil
}

// Has says whether the box sent that field.
func (c lcdConfig) Has(name string) bool { return c.present[name] }

// HasStrip is true when the box has a LED strip.
func (c lcdConfig) HasStrip() bool {
	return c.Has("led_strip_enabled") || c.Has("led_strip_brightness") || c.Has("led_strip_animation")
}

type choice struct {
	Value string
	Label string
}

var screensavers = []choice{
	{"disabled", "Écran toujours allumé"}, {"on", "Économiseur d'écran"}, {"night", "Économiseur la nuit"},
}

var orientations = []int{0, 90, 180, 270}

// Screensavers lists the choices for the template.
func (lcdConfig) Screensavers() []choice { return screensavers }

// Orientations lists the angles for the template.
func (lcdConfig) Orientations() []int { return orientations }

type ledstripStatus struct {
	UsePlanning bool  `json:"use_planning"`
	NextChange  int64 `json:"next_change"`
}

func (s *Server) display(w http.ResponseWriter, r *http.Request) {
	var cfg lcdConfig
	err := s.fetch(r.Context(), "lcd/config/", &cfg)
	data := map[string]any{"Cfg": cfg}
	if err == nil && cfg.HasStrip() {
		var st ledstripStatus
		if s.fetch(r.Context(), "ledstrip/status", &st) == nil {
			data["Strip"] = st
		}
	}
	s.show(w, r, "display", data, err)
}

func (s *Server) saveDisplay(w http.ResponseWriter, r *http.Request) {
	var cfg lcdConfig
	if err := s.fetch(r.Context(), "lcd/config/", &cfg); err != nil {
		setFlash(w, "err:"+err.Error())
		http.Redirect(w, r, "/display", http.StatusSeeOther)
		return
	}
	// Only send what this box has: the others would be refused.
	body := map[string]any{}
	if cfg.Has("brightness") {
		body["brightness"] = clamp(formInt(r, "brightness"), 0, 100)
	}
	if cfg.Has("orientation_forced") {
		body["orientation_forced"] = formBool(r, "orientation_forced")
	}
	if cfg.Has("orientation") {
		o := formInt(r, "orientation")
		if !slices.Contains(orientations, o) {
			o = cfg.Orientation
		}
		body["orientation"] = o
	}
	if cfg.Has("hide_wifi_key") {
		body["hide_wifi_key"] = formBool(r, "hide_wifi_key")
	}
	if cfg.Has("hide_status_led") {
		body["hide_status_led"] = formBool(r, "hide_status_led")
	}
	if cfg.Has("screensaver") {
		v := formStr(r, "screensaver")
		if slices.ContainsFunc(screensavers, func(c choice) bool { return c.Value == v }) {
			body["screensaver"] = v
		}
	}
	if cfg.Has("led_strip_enabled") {
		body["led_strip_enabled"] = formBool(r, "led_strip_enabled")
	}
	if cfg.Has("led_strip_brightness") {
		body["led_strip_brightness"] = clamp(formInt(r, "led_strip_brightness"), 0, 100)
	}
	if cfg.Has("led_strip_animation") {
		if v := formStr(r, "led_strip_animation"); slices.Contains(cfg.StripAnimations, v) {
			body["led_strip_animation"] = v
		}
	}
	s.change(w, r, http.MethodPut, "lcd/config/", body, "Réglages enregistrés.", "/display")
}

// ledstripPlanning switches the LED strip planning on or off, keeping the
// schedule the box already has.
func (s *Server) ledstripPlanning(w http.ResponseWriter, r *http.Request) {
	var plan map[string]any
	if err := s.fetch(r.Context(), "ledstrip/planning/", &plan); err != nil {
		setFlash(w, "err:"+err.Error())
		http.Redirect(w, r, "/display", http.StatusSeeOther)
		return
	}
	plan["use_planning"] = formBool(r, "use_planning")
	s.change(w, r, http.MethodPut, "ledstrip/planning/", plan, "Planning de la bande lumineuse enregistré.", "/display")
}

func clamp(n, lo, hi int) int { return min(max(n, lo), hi) }
