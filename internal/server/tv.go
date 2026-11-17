package server

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// TV: the channels of the main bouquet with what is on now and next (EPG),
// and a shortcut to schedule a recording. The TV API is not in the official
// documentation; the shapes below come from a real box (Freebox OS 4.13).

func init() {
	register(module{Slug: "tv", Title: "Chaînes et guide", Group: "Contenus", Order: 30, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /tv", s.tv)
		s.page(mux, "POST /tv/record", s.recordProgram)
	}})
	ops.Register(
		ops.Op{Name: "tv_bouquets_list", Description: `List the TV bouquets: id, name, channel_count, state. The main one is usually "Freebox TV".`,
			Path: "tv/bouquets/"},
		ops.Op{Name: "tv_bouquet_channels_list", Description: "List the channels of a bouquet: number, uuid, available, streams (quality, type, rtsp/hls URLs). Names are in tv_channels_list.",
			Path: "tv/bouquets/{id}/channels/", Params: []ops.Param{{Name: "id", Description: "Bouquet id from tv_bouquets_list"}}},
		ops.Op{Name: "tv_channels_list", Description: "List every known channel as an object indexed by uuid: name, short_name, available, has_service, has_abo, logo_url. Large answer.",
			Path: "tv/channels/"},
		ops.Op{Name: "tv_epg_by_time", Description: `Programs on air around a unix timestamp, for all channels at once: object indexed by channel uuid, then by program id ("<start>_<hash>"), with title, sub_title, desc, date (start), duration (s), next/prev program ids, category_name. Large answer.`,
			Path: "tv/epg/by_time/{timestamp}/", Params: []ops.Param{{Name: "timestamp", Description: "Unix timestamp, e.g. now"}}},
		ops.Op{Name: "tv_epg_by_channel", Description: "Programs of one channel around a unix timestamp, indexed by program id (same fields as tv_epg_by_time).",
			Path: "tv/epg/by_channel/{uuid}/{timestamp}/", Params: []ops.Param{{Name: "uuid", Description: `Channel uuid, e.g. "uuid-webtv-404"`}, {Name: "timestamp", Description: "Unix timestamp"}}},
		ops.Op{Name: "tv_epg_program_get", Description: "Get one program of the guide by its id (may answer an empty object when the program is not in the guide any more).",
			Path: "tv/epg/programs/{id}/", Params: []ops.Param{{Name: "id", Description: `Program id, "<start>_<hash>"`}}},
	)
}

// tvLimit caps the channels drawn by default: each one carries record forms.
const tvLimit = 100

type tvBouquet struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	ChannelCount int    `json:"channel_count"`
	State        string `json:"state"`
}

type tvBouquetChannel struct {
	Number    int    `json:"number"`
	UUID      string `json:"uuid"`
	Available bool   `json:"available"`
}

type tvChannelInfo struct {
	Name string `json:"name"`
}

type epgProgram struct {
	Title    string `json:"title"`
	SubTitle string `json:"sub_title"`
	Category string `json:"category_name"`
	Date     int64  `json:"date"`
	Duration int64  `json:"duration"`
	Next     string `json:"next"`
}

// tvProgram is a program as the page shows it.
type tvProgram struct {
	Title, SubTitle, Category string
	Start, End                int64
}

// Time is "20:50 – 22:10", in the server's time zone.
func (p tvProgram) Time() string {
	return time.Unix(p.Start, 0).Format("15:04") + " – " + time.Unix(p.End, 0).Format("15:04")
}

type tvRow struct {
	Number    int
	UUID      string
	Name      string
	Available bool
	Now, Next *tvProgram
}

func (s *Server) tv(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var bouquets []tvBouquet
	if err := s.fetch(ctx, "tv/bouquets/", &bouquets); err != nil {
		s.show(w, r, "tv", nil, err)
		return
	}
	if len(bouquets) == 0 {
		s.show(w, r, "tv", map[string]any{"Rows": []tvRow{}, "Bouquets": bouquets}, nil)
		return
	}
	sel := bouquets[0]
	if id, _ := strconv.Atoi(r.URL.Query().Get("b")); id != 0 {
		for _, b := range bouquets {
			if b.ID == id {
				sel = b
			}
		}
	}
	var chans []tvBouquetChannel
	if err := s.fetch(ctx, "tv/bouquets/"+strconv.Itoa(sel.ID)+"/channels/", &chans); err != nil {
		s.show(w, r, "tv", nil, err)
		return
	}
	var names map[string]tvChannelInfo
	if err := s.fetch(ctx, "tv/channels/", &names); err != nil {
		s.show(w, r, "tv", nil, err)
		return
	}
	// One request for the whole guide; a failure only empties the guide.
	now := time.Now().Unix()
	var epg map[string]map[string]epgProgram
	epgErr := s.fetch(ctx, "tv/epg/by_time/"+strconv.FormatInt(now, 10)+"/", &epg)

	all := r.URL.Query().Get("all") == "1"
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var rows []tvRow
	for _, c := range chans {
		if !all && !c.Available {
			continue
		}
		row := tvRow{Number: c.Number, UUID: c.UUID, Name: names[c.UUID].Name, Available: c.Available}
		if row.Name == "" {
			row.Name = c.UUID
		}
		if q != "" && !strings.Contains(strings.ToLower(row.Name+" "+strconv.Itoa(c.Number)), q) {
			continue
		}
		row.Now, row.Next = onAir(epg[c.UUID], now)
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Number < rows[j].Number })
	total := len(rows)
	full := r.URL.Query().Get("tout") == "1"
	if !full && total > tvLimit {
		rows = rows[:tvLimit]
	}
	data := map[string]any{"Rows": rows, "Bouquets": bouquets, "Bouquet": sel, "All": all, "Q": q, "Total": total, "Truncated": len(rows) < total}
	if epgErr != nil {
		s.log.Error("page", "page", "tv", "what", "epg", "err", epgErr)
		data["EPGError"] = epgErr.Error()
	}
	s.show(w, r, "tv", data, nil)
}

// onAir picks the program running at now and the one that follows it.
func onAir(progs map[string]epgProgram, now int64) (cur, next *tvProgram) {
	var nextID string
	for _, p := range progs {
		if p.Date <= now && now < p.Date+p.Duration {
			cur, nextID = toTVProgram(p), p.Next
			break
		}
	}
	if cur == nil {
		return nil, nil
	}
	if n, ok := progs[nextID]; ok {
		next = toTVProgram(n)
	}
	return cur, next
}

func toTVProgram(p epgProgram) *tvProgram {
	return &tvProgram{Title: p.Title, SubTitle: p.SubTitle, Category: p.Category, Start: p.Date, End: p.Date + p.Duration}
}

// recordProgram schedules a recording of a program shown on the page.
func (s *Server) recordProgram(w http.ResponseWriter, r *http.Request) {
	start, end := int64(formInt(r, "start")), int64(formInt(r, "end"))
	if now := time.Now().Unix(); start < now {
		start = now // the program already began: record what is left
	}
	uuid := formStr(r, "channel")
	if uuid == "" || end <= start {
		setFlash(w, "err:Ce programme est terminé ou incomplet, impossible de l'enregistrer.")
		http.Redirect(w, r, "/tv", http.StatusSeeOther)
		return
	}
	name := formStr(r, "name")
	if name == "" {
		name = fmt.Sprintf("Enregistrement %s", time.Unix(start, 0).Format("02/01 15:04"))
	}
	s.change(w, r, http.MethodPost, "pvr/programmed/",
		map[string]any{"channel_uuid": uuid, "start": start, "end": end, "name": name, "subname": formStr(r, "subname")},
		"Enregistrement programmé.", "/tv")
}
