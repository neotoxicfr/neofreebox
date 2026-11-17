package server

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Diagnostics: the push-notification targets registered on the box (phones
// and notification servers). Their tokens come from the mobile apps, so the
// page lists and removes them; adding or editing is left to the API and MCP.
//
// The slowness diagnostic of the API documentation has no endpoint in it
// (the section stops at a heading), so it is not covered.

func init() {
	register(module{Slug: "diagnostics", Title: "Diagnostic", Group: "Supervision", Order: 30, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /diagnostics", s.diagnostics)
		s.page(mux, "POST /diagnostics/targets/{id}/delete", s.deleteNotifTarget)
	}})
	const targetHelp = `{"name": "My server", "type": "firebase", "token": "device-or-service-token", "subscriptions": ["download", "phone"], "api_url": "https://notify.example.com/app", "message_type": "notification"}; type is ios, android or firebase; subscriptions among phone, download, security, box_state, lan_host, password_change; message_type is data or notification`
	ops.Register(
		ops.Op{Name: "notif_targets_list", Path: "notif/targets",
			Description: "List the notification targets: id, name, type (ios, android, firebase), subscriptions, api_url, message_type, last_use."},
		ops.Op{Name: "notif_target_get", Path: "notif/targets/{id}", Description: "Get one notification target.",
			Params: []ops.Param{{Name: "id", Description: "Target id (UUID)"}}},
		ops.Op{Name: "notif_target_add", Method: http.MethodPost, Path: "notif/targets/",
			Description: "Add a notification target. The box then calls api_url (POST /register, POST /send) to deliver notifications: every alert goes to that URL.",
			Body:        true, BodyHelp: targetHelp, Dangerous: true},
		ops.Op{Name: "notif_target_update", Method: http.MethodPut, Path: "notif/targets/{id}", Description: "Update a notification target (same fields as notif_target_add).",
			Params: []ops.Param{{Name: "id", Description: "Target id (UUID)"}}, Body: true, BodyHelp: targetHelp, Dangerous: true},
		ops.Op{Name: "notif_target_delete", Method: http.MethodDelete, Path: "notif/targets/{id}", Dangerous: true,
			Description: "Delete a notification target: that phone or server stops receiving the box's alerts until it registers again.",
			Params:      []ops.Param{{Name: "id", Description: "Target id (UUID)"}}},
	)
}

type notifTarget struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	LastUse       int64    `json:"last_use"`
	APIURL        string   `json:"api_url"`
	MessageType   string   `json:"message_type"`
	Subscriptions []string `json:"subscriptions"`
}

// Events lists the subscriptions in words.
func (t notifTarget) Events() string {
	names := map[string]string{"phone": "appels manqués", "download": "téléchargements", "downloader": "téléchargements",
		"security": "alarme", "box_state": "état de la box", "lan_host": "réseau local", "password_change": "mot de passe"}
	var out []string
	for _, s := range t.Subscriptions {
		if n, ok := names[s]; ok {
			s = n
		}
		out = append(out, s)
	}
	return strings.Join(out, ", ")
}

func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	var targets []notifTarget
	err := s.fetch(r.Context(), "notif/targets", &targets)
	s.show(w, r, "diagnostics", map[string]any{"Targets": targets}, err)
}

func (s *Server) deleteNotifTarget(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/diagnostics") {
		return
	}
	s.change(w, r, http.MethodDelete, "notif/targets/"+url.PathEscape(r.PathValue("id")), nil,
		"Cible de notification supprimée.", "/diagnostics")
}
