package server

import (
	"net/http"
	"net/url"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Calls: the call log, the phone account and the voicemail. Phone numbers
// are personal data: they are shown but never logged (only ids go in paths).
//
// The voicemail audio file (call/voicemail/{id}/audio_file) is binary WAV;
// the box client only decodes JSON, so it is neither a page nor an operation.

func init() {
	register(module{Slug: "calls", Title: "Appels", Group: "Téléphonie", Order: 0, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /calls", s.calls)
		s.page(mux, "POST /calls/read-all", s.callsReadAll)
		s.page(mux, "POST /calls/delete-all", s.callsDeleteAll)
		s.page(mux, "POST /calls/{id}/read", s.callRead)
		s.page(mux, "POST /calls/{id}/delete", s.callDelete)
		s.page(mux, "POST /calls/voicemail/{id}/read", s.voicemailRead)
		s.page(mux, "POST /calls/voicemail/{id}/delete", s.voicemailDelete)
	}})
	callID := ops.Param{Name: "id", Description: "Call entry id (integer)"}
	vmID := ops.Param{Name: "id", Description: `Voicemail id, e.g. "20250101_120000_r0199000000.au"`}
	ops.Register(
		ops.Op{Name: "call_log_list", Description: "List the call log: id, type (missed, accepted, outgoing), datetime, number, name, duration in seconds, new (not acknowledged), contact_id.",
			Path: "call/log/"},
		ops.Op{Name: "call_log_get", Description: "Get one call log entry.", Path: "call/log/{id}/", Params: []ops.Param{callID}},
		ops.Op{Name: "call_log_update", Description: "Update a call log entry; set new to false to mark it as read.", Method: http.MethodPut,
			Path: "call/log/{id}/", Params: []ops.Param{callID}, Body: true, BodyHelp: `{"new": false}`},
		ops.Op{Name: "call_log_delete", Description: "Delete one call log entry.", Method: http.MethodDelete,
			Path: "call/log/{id}/", Params: []ops.Param{callID}},
		ops.Op{Name: "call_log_mark_all_read", Description: "Mark every call log entry as read.", Method: http.MethodPost,
			Path: "call/log/mark_all_as_read/"},
		ops.Op{Name: "call_log_delete_all", Description: "Delete the whole call log. The entries cannot be recovered.", Method: http.MethodPost,
			Path: "call/log/delete_all/", Dangerous: true},
		ops.Op{Name: "call_account_get", Description: "Get the phone account: the phone number of the subscription (phone_number).",
			Path: "call/account/"},
		ops.Op{Name: "voicemail_list", Description: "List the voicemail messages: id, phone_number, country_code, date, duration in seconds, read.",
			Path: "call/voicemail/"},
		ops.Op{Name: "voicemail_get", Description: "Get one voicemail message.", Path: "call/voicemail/{id}/", Params: []ops.Param{vmID}},
		ops.Op{Name: "voicemail_update", Description: "Update a voicemail message; set read to true to mark it as read.", Method: http.MethodPut,
			Path: "call/voicemail/{id}/", Params: []ops.Param{vmID}, Body: true, BodyHelp: `{"read": true}`},
		ops.Op{Name: "voicemail_delete", Description: "Delete one voicemail message and its recording. It cannot be recovered.", Method: http.MethodDelete,
			Path: "call/voicemail/{id}/", Params: []ops.Param{vmID}, Dangerous: true},
	)
}

type callEntry struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	Datetime  int64  `json:"datetime"`
	Number    string `json:"number"`
	Name      string `json:"name"`
	Duration  int64  `json:"duration"`
	New       bool   `json:"new"`
	ContactID int    `json:"contact_id"`
}

// Label is the call direction, in French.
func (c callEntry) Label() string {
	switch c.Type {
	case "missed":
		return "Manqué"
	case "accepted":
		return "Reçu"
	case "outgoing":
		return "Émis"
	}
	return c.Type
}

// Who is the name to show, falling back to the number.
func (c callEntry) Who() string {
	if c.Name != "" {
		return c.Name
	}
	return c.Number
}

type voicemail struct {
	ID          string `json:"id"`
	CountryCode any    `json:"country_code"` // the box sends a string or a number
	PhoneNumber string `json:"phone_number"`
	Date        int64  `json:"date"`
	Duration    int64  `json:"duration"`
	Read        bool   `json:"read"`
}

func (s *Server) calls(w http.ResponseWriter, r *http.Request) {
	var log []callEntry
	var boxes []voicemail
	var acct struct {
		PhoneNumber string `json:"phone_number"`
	}
	err := s.fetch(r.Context(), "call/log/", &log)
	if err == nil {
		// The account and the voicemail are secondary: the page works without them.
		if e := s.fetch(r.Context(), "call/account/", &acct); e != nil {
			s.log.Warn("calls: account", "err", e)
		}
		if e := s.fetch(r.Context(), "call/voicemail/", &boxes); e != nil {
			s.log.Warn("calls: voicemail", "err", e)
		}
	}
	unread, unreadVM := 0, 0
	for _, c := range log {
		if c.New {
			unread++
		}
	}
	for _, v := range boxes {
		if !v.Read {
			unreadVM++
		}
	}
	s.show(w, r, "calls", map[string]any{"Calls": log, "Unread": unread, "Voicemails": boxes, "UnreadVM": unreadVM, "Phone": acct.PhoneNumber}, err)
}

func (s *Server) callsReadAll(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodPost, "call/log/mark_all_as_read/", nil, "Tous les appels sont marqués comme lus.", "/calls")
}

func (s *Server) callsDeleteAll(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/calls") {
		return
	}
	s.change(w, r, http.MethodPost, "call/log/delete_all/", nil, "Journal d'appels vidé.", "/calls")
}

func (s *Server) callRead(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodPut, "call/log/"+url.PathEscape(r.PathValue("id"))+"/", map[string]bool{"new": false}, "Appel marqué comme lu.", "/calls")
}

func (s *Server) callDelete(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodDelete, "call/log/"+url.PathEscape(r.PathValue("id"))+"/", nil, "Appel supprimé.", "/calls")
}

func (s *Server) voicemailRead(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodPut, "call/voicemail/"+url.PathEscape(r.PathValue("id"))+"/", map[string]bool{"read": true}, "Message marqué comme lu.", "/calls")
}

func (s *Server) voicemailDelete(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/calls") {
		return
	}
	s.change(w, r, http.MethodDelete, "call/voicemail/"+url.PathEscape(r.PathValue("id"))+"/", nil, "Message supprimé.", "/calls")
}
