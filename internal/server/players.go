package server

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Players: the Freebox Player (TV boxes) seen on the LAN, and their remote
// control. The API has no restart for a player.

func init() {
	register(module{Slug: "players", Title: "Players", Group: "Système", Order: 40, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /players", s.players)
		s.page(mux, "POST /players/{id}/control", s.playerControl)
		s.page(mux, "POST /players/{id}/volume", s.playerVolume)
		s.page(mux, "POST /players/{id}/open", s.playerOpen)
	}})
	idParam := ops.Param{Name: "id_player", Description: "Player id from player_list"}
	ops.Register(
		ops.Op{Name: "player_list", Description: "List the Freebox Player devices on the LAN: id (when the box gives one), device_name, device_model, stb_type, mac, reachable, api_available.",
			Path: "player/"},
		ops.Op{Name: "player_status_get", Description: "Get a player's status: power_state (standby or running), the active media player and its capabilities, the foreground app.",
			Path: "player/{id_player}/api/v6/status/", Params: []ops.Param{idParam}},
		ops.Op{Name: "player_control", Description: "Send a command to the active media player of a player device (not always available: see capabilities in the status).", Method: http.MethodPost,
			Path: "player/{id_player}/api/v6/control/mediactrl/", Params: []ops.Param{idParam},
			Body: true, BodyHelp: `{"cmd": "play_pause"} with cmd one of play_pause, stop, prev, next, select_stream, select_audio_track, select_srt_track`},
		ops.Op{Name: "player_volume_get", Description: "Get a player's volume (0-100) and mute state.",
			Path: "player/{id_player}/api/v6/control/volume/", Params: []ops.Param{idParam}},
		ops.Op{Name: "player_volume_set", Description: "Set a player's volume (0-100) and/or mute.", Method: http.MethodPut,
			Path: "player/{id_player}/api/v6/control/volume/", Params: []ops.Param{idParam},
			Body: true, BodyHelp: `{"volume": 50, "mute": false}`},
		ops.Op{Name: "player_open", Description: "Open a URL on a player: a video, a web page, a TV channel (tv:?channel=2) or a YouTube video.", Method: http.MethodPost,
			Path: "player/{id_player}/api/v6/control/open", Params: []ops.Param{idParam},
			Body: true, BodyHelp: `{"url": "tv:?channel=2", "type": ""}`},
	)
}

type player struct {
	ID           int    `json:"id"`
	DeviceName   string `json:"device_name"`
	DeviceModel  string `json:"device_model"`
	STBType      string `json:"stb_type"`
	MAC          string `json:"mac"`
	Reachable    bool   `json:"reachable"`
	APIAvailable bool   `json:"api_available"`
	APIVersion   string `json:"api_version"`
	LastReach    int64  `json:"last_time_reachable"`

	Status *playerStatus `json:"-"`
	Volume *playerVolume `json:"-"`
}

// Controllable is true when the box can relay commands to this player.
func (p player) Controllable() bool { return p.ID > 0 && p.APIAvailable && p.Reachable }

type playerStatus struct {
	PowerState string `json:"power_state"`
	Player     struct {
		Name string `json:"name"`
	} `json:"player"`
}

// Power is the player's power state in French.
func (st playerStatus) Power() string {
	switch st.PowerState {
	case "standby":
		return "en veille"
	case "running":
		return "allumé"
	}
	return st.PowerState
}

type playerVolume struct {
	Volume int  `json:"volume"`
	Mute   bool `json:"mute"`
}

func (s *Server) players(w http.ResponseWriter, r *http.Request) {
	var list []player
	err := s.fetch(r.Context(), "player/", &list)
	for i := range list {
		p := &list[i]
		if !p.Controllable() {
			continue
		}
		base := "player/" + strconv.Itoa(p.ID) + "/api/v6/"
		var st playerStatus
		if s.fetch(r.Context(), base+"status/", &st) == nil {
			p.Status = &st
		}
		var v playerVolume
		if s.fetch(r.Context(), base+"control/volume/", &v) == nil {
			p.Volume = &v
		}
	}
	s.show(w, r, "players", map[string]any{"Players": list}, err)
}

// playerBase is the API path of a player, or "" when the id is not a number.
func playerBase(r *http.Request) string {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		return ""
	}
	return "player/" + strconv.Itoa(id) + "/api/v6/"
}

var playerCommands = []string{"play_pause", "stop", "prev", "next"}

func (s *Server) playerControl(w http.ResponseWriter, r *http.Request) {
	base, cmd := playerBase(r), formStr(r, "cmd")
	if base == "" || !slices.Contains(playerCommands, cmd) {
		setFlash(w, "err:Commande invalide.")
		http.Redirect(w, r, "/players", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPost, base+"control/mediactrl/", map[string]string{"cmd": cmd}, "Commande envoyée.", "/players")
}

func (s *Server) playerVolume(w http.ResponseWriter, r *http.Request) {
	base := playerBase(r)
	if base == "" {
		setFlash(w, "err:Player invalide.")
		http.Redirect(w, r, "/players", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, base+"control/volume/",
		map[string]any{"volume": clamp(formInt(r, "volume"), 0, 100), "mute": formBool(r, "mute")}, "Volume enregistré.", "/players")
}

func (s *Server) playerOpen(w http.ResponseWriter, r *http.Request) {
	base, target := playerBase(r), formStr(r, "url")
	if u, err := url.Parse(target); base == "" || target == "" || err != nil || u.Scheme == "" {
		setFlash(w, "err:Adresse invalide (exemples : tv:?channel=2, https://…).")
		http.Redirect(w, r, "/players", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPost, base+"control/open", map[string]string{"url": target}, "Adresse ouverte sur le player.", "/players")
}
