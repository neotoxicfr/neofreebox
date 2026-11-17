package server

import (
	"net/http"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// System: box details, firmware update status, language, reboot and shutdown.

func init() {
	register(module{Slug: "system", Title: "Box", Group: "Système", Order: 0, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /system", s.system)
		s.page(mux, "POST /system/lang", s.setLang)
		s.page(mux, "POST /system/reboot", s.reboot)
		s.page(mux, "POST /system/shutdown", s.shutdown)
	}})
	ops.Register(
		ops.Op{Name: "system_get", Description: "Get the box details: model_info, firmware_version, serial, mac, uptime_val (s), board_name, disk_status, usb3_enable, temperature sensors and fans.",
			Path: "system/"},
		ops.Op{Name: "system_reboot", Description: "Reboot the Freebox. The network is down for a few minutes.", Method: http.MethodPost,
			Path: "system/reboot/", Dangerous: true},
		ops.Op{Name: "system_shutdown", Description: "Shut the Freebox down. It must be switched on again by hand.", Method: http.MethodPost,
			Path: "system/shutdown/", Dangerous: true},
		ops.Op{Name: "update_status_get", Description: "Get the firmware update status: state (initializing, upgrading, up_to_date, error) and upgrade_state while upgrading. The API cannot start an update.",
			Path: "update/"},
		ops.Op{Name: "lang_get", Description: "Get the box language (lang, ISO 639-3) and the supported languages (avalaible, sic).",
			Path: "lang/"},
		ops.Op{Name: "lang_set", Description: "Set the box language.", Method: http.MethodPost,
			Path: "lang/", Body: true, BodyHelp: `{"lang": "fra"}`},
	)
}

type systemDetails struct {
	MAC             string `json:"mac"`
	Serial          string `json:"serial"`
	BoardName       string `json:"board_name"`
	FirmwareVersion string `json:"firmware_version"`
	Uptime          int64  `json:"uptime_val"`
	DiskStatus      string `json:"disk_status"`
	USB3            bool   `json:"usb3_enable"`
	ModelInfo       struct {
		PrettyName string `json:"pretty_name"`
	} `json:"model_info"`
	Sensors []reading `json:"sensors"`
	Fans    []reading `json:"fans"`
}

var diskStates = map[string]string{
	"not_detected": "non détecté", "disabled": "désactivé", "initializing": "initialisation",
	"error": "erreur de montage", "active": "actif",
}

// Disk is the internal disk state in French.
func (d systemDetails) Disk() string {
	if l, ok := diskStates[d.DiskStatus]; ok {
		return l
	}
	return d.DiskStatus
}

type updateStatus struct {
	State        string `json:"state"`
	UpgradeState struct {
		State      string `json:"state"`
		OldVersion string `json:"old_version"`
		NewVersion string `json:"new_version"`
		Percent    int    `json:"percent"`
		Error      string `json:"error_string"`
	} `json:"upgrade_state"`
}

// Label is the update state in French.
func (u updateStatus) Label() string {
	switch u.State {
	case "up_to_date", "auto_up_to_date":
		return "À jour"
	case "initializing":
		return "Préparation de la mise à jour"
	case "upgrading":
		return "Mise à jour en cours"
	case "error":
		return "Erreur de mise à jour"
	case "":
		return "Inconnu"
	}
	return u.State
}

// Busy is true while an update is running.
func (u updateStatus) Busy() bool { return u.State == "initializing" || u.State == "upgrading" }

// Bad is true when the update failed.
func (u updateStatus) Bad() bool { return u.State == "error" }

type langInfo struct {
	Lang      string   `json:"lang"`
	Available []string `json:"avalaible"` // sic, the box spells it so
}

var langNames = map[string]string{"fra": "Français", "eng": "English", "ita": "Italiano", "deu": "Deutsch", "spa": "Español"}

func langName(code string) string {
	if n, ok := langNames[code]; ok {
		return n
	}
	return code
}

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	var sys systemDetails
	err := s.fetch(r.Context(), "system/", &sys)
	data := map[string]any{"Sys": sys}
	if err == nil {
		// Update and language are secondary: the page works without them.
		var (
			upd  updateStatus
			lang langInfo
		)
		data["UpdateErr"] = s.fetch(r.Context(), "update/", &upd) != nil
		data["LangErr"] = s.fetch(r.Context(), "lang/", &lang) != nil
		data["Update"] = upd
		langs := make([]map[string]any, 0, len(lang.Available))
		for _, l := range lang.Available {
			langs = append(langs, map[string]any{"Code": l, "Name": langName(l), "Current": l == lang.Lang})
		}
		data["Langs"] = langs
	}
	s.show(w, r, "system", data, err)
}

func (s *Server) setLang(w http.ResponseWriter, r *http.Request) {
	lang := formStr(r, "lang")
	if len(lang) != 3 {
		setFlash(w, "err:Langue invalide.")
		http.Redirect(w, r, "/system", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPost, "lang/", map[string]string{"lang": lang}, "Langue enregistrée.", "/system")
}

func (s *Server) reboot(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/system") {
		return
	}
	s.change(w, r, http.MethodPost, "system/reboot/", nil, "Redémarrage lancé : la box sera de retour dans quelques minutes.", "/system")
}

func (s *Server) shutdown(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/system") {
		return
	}
	s.change(w, r, http.MethodPost, "system/shutdown/", nil, "Extinction lancée : il faudra rallumer la box à la main.", "/system")
}
