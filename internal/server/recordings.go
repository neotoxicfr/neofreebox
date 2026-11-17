package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Recordings: PVR settings, scheduled recordings (programmed), finished
// recordings and the storage media they are written to.

func init() {
	register(module{Slug: "recordings", Title: "Enregistrements", Group: "Contenus", Order: 40, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /recordings", s.recordings)
		s.page(mux, "POST /recordings/config", s.pvrConfig)
		s.page(mux, "POST /recordings/quota", s.pvrQuota)
		s.page(mux, "POST /recordings/programmed", s.addPrecord)
		s.page(mux, "POST /recordings/programmed/{id}/update", s.updatePrecord)
		s.page(mux, "POST /recordings/programmed/{id}/enabled", s.enablePrecord)
		s.page(mux, "POST /recordings/programmed/{id}/delete", s.deletePrecord)
		s.page(mux, "POST /recordings/finished/{id}/rename", s.renameFrecord)
		s.page(mux, "POST /recordings/finished/{id}/delete", s.deleteFrecord)
	}})
	idParam := []ops.Param{{Name: "id", Description: "Record id"}}
	ops.Register(
		ops.Op{Name: "pvr_config_get", Description: "Get the PVR configuration: margin_before and margin_after (minutes added around recordings).",
			Path: "pvr/config/"},
		ops.Op{Name: "pvr_config_update", Description: "Update the default margins around recordings.", Method: http.MethodPut,
			Path: "pvr/config/", Body: true, BodyHelp: `{"margin_before": 2, "margin_after": 5}`},
		ops.Op{Name: "pvr_quota_get", Description: "Get the recording quota state: quota_exceeded, needed_tresh, cur_tresh.",
			Path: "pvr/quota/"},
		ops.Op{Name: "pvr_quota_request", Description: "Ask for the next quota threshold; the box adjusts it automatically if needed.", Method: http.MethodPut,
			Path: "pvr/quota/", Body: true, BodyHelp: `{}`},
		ops.Op{Name: "pvr_programmed_list", Description: "List scheduled recordings: id, name, subname, channel_uuid, channel_name, start, end (unix), state, error, conflict, enabled, media, path, has_record_gen.",
			Path: "pvr/programmed/"},
		ops.Op{Name: "pvr_programmed_get", Description: "Get one scheduled recording.",
			Path: "pvr/programmed/{id}/", Params: idParam},
		ops.Op{Name: "pvr_programmed_create", Description: "Schedule a recording of a channel between two unix timestamps.", Method: http.MethodPost,
			Path: "pvr/programmed/", Body: true, BodyHelp: `{"channel_uuid": "uuid-webtv-374", "start": 1444240500, "end": 1444244100, "name": "Film", "subname": "Soirée"}`},
		ops.Op{Name: "pvr_programmed_update", Description: "Change a manual scheduled recording (name, subname, start, end, channel_uuid) or enable or disable a generated one (enabled).", Method: http.MethodPut,
			Path: "pvr/programmed/{id}/", Params: idParam, Body: true, BodyHelp: `{"name": "New name", "start": 1444240500, "end": 1444244100}`},
		ops.Op{Name: "pvr_programmed_delete", Description: "Delete a scheduled recording (or cancel a running one).", Method: http.MethodDelete,
			Path: "pvr/programmed/{id}/", Params: idParam},
		ops.Op{Name: "pvr_finished_list", Description: "List finished or running recordings: id, name, subname, channel_name, start, end, byte_size, filename, media, path, state, error, altered, secure.",
			Path: "pvr/finished/"},
		ops.Op{Name: "pvr_finished_get", Description: "Get one finished recording.",
			Path: "pvr/finished/{id}/", Params: idParam},
		ops.Op{Name: "pvr_finished_update", Description: "Rename a finished recording (name, subname).", Method: http.MethodPut,
			Path: "pvr/finished/{id}/", Params: idParam, Body: true, BodyHelp: `{"name": "Tout le sport", "subname": "On est les champions"}`},
		ops.Op{Name: "pvr_finished_delete", Description: "Delete a finished recording AND its video file from the disk. Irreversible.", Method: http.MethodDelete,
			Path: "pvr/finished/{id}/", Params: idParam, Dangerous: true},
		ops.Op{Name: "pvr_media_list", Description: "List the storage media recordings are written to: media name, free_bytes, total_bytes, record_time (estimated seconds per stream type and quality).",
			Path: "pvr/media/"},
	)
}

type pvrConfig struct {
	MarginBefore int `json:"margin_before"`
	MarginAfter  int `json:"margin_after"`
}

type pvrQuota struct {
	Exceeded bool `json:"quota_exceeded"`
	Needed   int  `json:"needed_tresh"`
	Current  int  `json:"cur_tresh"`
}

type pvrMedia struct {
	Media string `json:"media"`
	Free  int64  `json:"free_bytes"`
	Total int64  `json:"total_bytes"`
}

// Used is the percentage of the medium that is full.
func (m pvrMedia) Used() int {
	if m.Total <= 0 {
		return 0
	}
	return int((m.Total - m.Free) * 100 / m.Total)
}

// record is both a scheduled (programmed) and a finished record.
type record struct {
	ID          json.Number `json:"id"`
	Name        string      `json:"name"`
	SubName     string      `json:"subname"`
	ChannelName string      `json:"channel_name"`
	Start       int64       `json:"start"`
	End         int64       `json:"end"`
	State       string      `json:"state"`
	Error       string      `json:"error"`
	Conflict    bool        `json:"conflict"`
	Altered     bool        `json:"altered"`
	Enabled     bool        `json:"enabled"`
	Generated   bool        `json:"has_record_gen"`
	Media       string      `json:"media"`
	ByteSize    int64       `json:"byte_size"`
	Filename    string      `json:"filename"`
}

const inputTime = "2006-01-02T15:04" // value of <input type="datetime-local">

// StartInput and EndInput fill datetime-local fields.
func (r record) StartInput() string { return time.Unix(r.Start, 0).Format(inputTime) }
func (r record) EndInput() string   { return time.Unix(r.End, 0).Format(inputTime) }

// StateLabel is the record's state in French, with its error if any.
func (r record) StateLabel() string {
	l, ok := map[string]string{
		"disabled": "désactivé", "start_error": "échec au démarrage", "waiting_start_time": "programmé", "starting": "démarrage",
		"running": "en cours", "running_error": "en cours, avec erreur", "failed": "échec", "finished": "terminé",
	}[r.State]
	if !ok {
		l = r.State
	}
	if e, ok := recordErrors[r.Error]; ok {
		l += " (" + e + ")"
	}
	return l
}

// StateOK is false for the states that deserve attention.
func (r record) StateOK() bool {
	switch r.State {
	case "start_error", "running_error", "failed", "disabled":
		return false
	}
	return !r.Conflict
}

var recordErrors = map[string]string{
	"file_access_error": "accès au fichier impossible", "disk_full": "disque plein", "private_but_no_private_dir": "dossier privé absent",
	"network_problem": "problème réseau", "resource_problem": "ressources insuffisantes", "no_stream_available": "aucun flux disponible",
	"no_data_received": "aucune donnée reçue", "missed": "manqué", "stopped": "arrêté", "internal_error": "erreur interne", "unknown_error": "erreur inconnue",
}

type channelChoice struct {
	UUID, Label string
}

// channelChoices lists the available channels of the main bouquet for the
// "add" form; on any failure the form falls back to a free uuid field.
func (s *Server) channelChoices(ctx context.Context) []channelChoice {
	var bouquets []tvBouquet
	if s.fetch(ctx, "tv/bouquets/", &bouquets) != nil || len(bouquets) == 0 {
		return nil
	}
	var chans []tvBouquetChannel
	var names map[string]tvChannelInfo
	if s.fetch(ctx, "tv/bouquets/"+strconv.Itoa(bouquets[0].ID)+"/channels/", &chans) != nil || s.fetch(ctx, "tv/channels/", &names) != nil {
		return nil
	}
	sort.SliceStable(chans, func(i, j int) bool { return chans[i].Number < chans[j].Number })
	var out []channelChoice
	for _, c := range chans {
		if c.Available && names[c.UUID].Name != "" {
			out = append(out, channelChoice{c.UUID, strconv.Itoa(c.Number) + " · " + names[c.UUID].Name})
		}
	}
	return out
}

func (s *Server) recordings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var cfg pvrConfig
	var quota pvrQuota
	var media []pvrMedia
	var programmed, finished []record
	err := firstErr(
		s.fetch(ctx, "pvr/config/", &cfg),
		s.fetch(ctx, "pvr/quota/", &quota),
		s.fetch(ctx, "pvr/media/", &media),
		s.fetch(ctx, "pvr/programmed/", &programmed),
		s.fetch(ctx, "pvr/finished/", &finished),
	)
	if err != nil {
		s.show(w, r, "recordings", nil, err)
		return
	}
	sort.SliceStable(programmed, func(i, j int) bool { return programmed[i].Start < programmed[j].Start })
	sort.SliceStable(finished, func(i, j int) bool { return finished[i].Start > finished[j].Start })
	now := time.Now()
	s.show(w, r, "recordings", map[string]any{
		"Config": cfg, "Quota": quota, "Media": media, "Programmed": programmed, "Finished": finished,
		"Channels": s.channelChoices(ctx),
		"DefStart": now.Add(time.Hour).Truncate(time.Hour).Format(inputTime),
		"DefEnd":   now.Add(2 * time.Hour).Truncate(time.Hour).Format(inputTime),
	}, nil)
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func (s *Server) pvrConfig(w http.ResponseWriter, r *http.Request) {
	before, after := formInt(r, "margin_before"), formInt(r, "margin_after")
	if before < 0 || after < 0 {
		setFlash(w, "err:Les marges ne peuvent pas être négatives.")
		http.Redirect(w, r, "/recordings", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "pvr/config/", map[string]int{"margin_before": before, "margin_after": after},
		"Marges enregistrées.", "/recordings")
}

func (s *Server) pvrQuota(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodPut, "pvr/quota/", map[string]any{}, "Palier de quota demandé.", "/recordings")
}

// formTime reads a datetime-local field, in the server's time zone.
func formTime(r *http.Request, name string) (int64, bool) {
	t, err := time.ParseInLocation(inputTime, formStr(r, name), time.Local)
	return t.Unix(), err == nil
}

func badRange(w http.ResponseWriter, r *http.Request, start, end int64, ok bool) bool {
	if ok && end > start {
		return false
	}
	setFlash(w, "err:Dates invalides : la fin doit suivre le début.")
	http.Redirect(w, r, "/recordings", http.StatusSeeOther)
	return true
}

func (s *Server) addPrecord(w http.ResponseWriter, r *http.Request) {
	start, ok1 := formTime(r, "start")
	end, ok2 := formTime(r, "end")
	if badRange(w, r, start, end, ok1 && ok2) {
		return
	}
	uuid, name := formStr(r, "channel"), formStr(r, "name")
	if uuid == "" || name == "" {
		setFlash(w, "err:Choisis une chaîne et donne un nom à l'enregistrement.")
		http.Redirect(w, r, "/recordings", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPost, "pvr/programmed/",
		map[string]any{"channel_uuid": uuid, "start": start, "end": end, "name": name, "subname": formStr(r, "subname")},
		"Enregistrement programmé.", "/recordings")
}

func (s *Server) updatePrecord(w http.ResponseWriter, r *http.Request) {
	start, ok1 := formTime(r, "start")
	end, ok2 := formTime(r, "end")
	if badRange(w, r, start, end, ok1 && ok2) {
		return
	}
	s.change(w, r, http.MethodPut, "pvr/programmed/"+url.PathEscape(r.PathValue("id"))+"/",
		map[string]any{"name": formStr(r, "name"), "subname": formStr(r, "subname"), "start": start, "end": end},
		"Enregistrement modifié.", "/recordings")
}

func (s *Server) enablePrecord(w http.ResponseWriter, r *http.Request) {
	on := formBool(r, "enabled")
	msg := "Enregistrement désactivé."
	if on {
		msg = "Enregistrement activé."
	}
	s.change(w, r, http.MethodPut, "pvr/programmed/"+url.PathEscape(r.PathValue("id"))+"/", map[string]bool{"enabled": on}, msg, "/recordings")
}

func (s *Server) deletePrecord(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodDelete, "pvr/programmed/"+url.PathEscape(r.PathValue("id"))+"/", nil, "Enregistrement supprimé.", "/recordings")
}

func (s *Server) renameFrecord(w http.ResponseWriter, r *http.Request) {
	name := formStr(r, "name")
	if name == "" {
		setFlash(w, "err:Le nom ne peut pas être vide.")
		http.Redirect(w, r, "/recordings", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "pvr/finished/"+url.PathEscape(r.PathValue("id"))+"/",
		map[string]string{"name": name, "subname": formStr(r, "subname")}, "Enregistrement renommé.", "/recordings")
}

func (s *Server) deleteFrecord(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/recordings") {
		return
	}
	s.change(w, r, http.MethodDelete, "pvr/finished/"+url.PathEscape(r.PathValue("id"))+"/", nil, "Enregistrement et vidéo supprimés.", "/recordings")
}
