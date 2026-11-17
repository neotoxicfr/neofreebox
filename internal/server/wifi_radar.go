package server

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
)

// Wi-Fi radar: for one radio, the connected stations, the neighbour
// networks, the channel usage and the channels the radio accepts.

func init() {
	register(module{Slug: "wifi-radar", Title: "Voisinage et stations", Group: "Wi-Fi", Order: 40, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /wifi-radar", s.wifiRadar)
		s.page(mux, "POST /wifi-radar/scan", s.wifiRadarScan)
	}})
}

type wifiStation struct {
	MAC      string `json:"mac"`
	Hostname string `json:"hostname"`
	State    string `json:"state"`
	Signal   int    `json:"signal"`
	Inactive int64  `json:"inactive"`
	Conn     int64  `json:"conn_duration"`
	RX       int64  `json:"rx_bytes"`
	TX       int64  `json:"tx_bytes"`
	LastTX   struct {
		Bitrate int `json:"bitrate"`
	} `json:"last_tx"`
	Host struct {
		Name string `json:"primary_name"`
	} `json:"host"`
}

// Name prefers the name known to the LAN browser.
func (s wifiStation) Name() string {
	if s.Host.Name != "" {
		return s.Host.Name
	}
	return s.Hostname
}

// Rate is the physical link rate towards the station (the box reports tenths of Mb/s).
func (s wifiStation) Rate() string {
	if s.LastTX.Bitrate <= 0 {
		return ""
	}
	return fmt.Sprintf("%.0f Mb/s", float64(s.LastTX.Bitrate)/10)
}

type wifiNeighbor struct {
	SSID      string  `json:"ssid"`
	BSSID     string  `json:"bssid"`
	Band      string  `json:"band"`
	Channel   int     `json:"channel"`
	Secondary int     `json:"secondary_channel"`
	Width     flexStr `json:"channel_width"`
	Signal    int     `json:"signal"`
}

type wifiChannelUse struct {
	Band  string `json:"band"`
	Chan  int    `json:"channel"`
	Noise int    `json:"noise_level"`
	Busy  int    `json:"rx_busy_percent"`
}

type wifiComb struct {
	Band    string  `json:"band"`
	Width   flexStr `json:"channel_width"`
	DFS     bool    `json:"need_dfs"`
	CAC     int     `json:"dfs_cac_time"`
	Primary int     `json:"primary"`
	Second  int     `json:"secondary"`
}

// combGroup lists the channels allowed for one width.
type combGroup struct {
	Width    string
	Channels []string
}

func groupCombs(list []wifiComb) []combGroup {
	var groups []combGroup
	idx := map[string]int{}
	for _, c := range list {
		i, ok := idx[string(c.Width)]
		if !ok {
			i = len(groups)
			idx[string(c.Width)] = i
			groups = append(groups, combGroup{Width: string(c.Width)})
		}
		label := strconv.Itoa(c.Primary)
		if c.Second != 0 {
			label += "+" + strconv.Itoa(c.Second)
		}
		if c.DFS {
			label += " (DFS)"
		}
		groups[i].Channels = append(groups[i].Channels, label)
	}
	return groups
}

func (s *Server) wifiRadar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var aps []wifiAP
	err := s.fetch(ctx, "wifi/ap/", &aps)
	data := map[string]any{"APs": aps}
	if err == nil && len(aps) > 0 {
		sel := aps[0]
		for _, a := range aps {
			if strconv.Itoa(a.ID) == r.URL.Query().Get("ap") {
				sel = a
			}
		}
		base := "wifi/ap/" + strconv.Itoa(sel.ID)
		var stations []wifiStation
		var neighbors []wifiNeighbor
		var usage []wifiChannelUse
		var combs []wifiComb
		s.soft(ctx, base+"/stations/", &stations)
		s.soft(ctx, base+"/neighbors/", &neighbors)
		s.soft(ctx, base+"/channel_usage/", &usage)
		s.soft(ctx, base+"/allowed_channel_comb", &combs)
		sort.SliceStable(neighbors, func(i, j int) bool { return neighbors[i].Signal > neighbors[j].Signal })
		sort.SliceStable(stations, func(i, j int) bool { return stations[i].Signal > stations[j].Signal })
		data["Sel"], data["Stations"], data["Neighbors"], data["Usage"], data["Combs"] = sel, stations, neighbors, usage, groupCombs(combs)
	}
	s.show(w, r, "wifi-radar", data, err)
}

func (s *Server) wifiRadarScan(w http.ResponseWriter, r *http.Request) {
	id, ok := radarForm(w, r)
	if !ok || !confirmed(w, r, "/wifi-radar?ap="+strconv.Itoa(id)) {
		return
	}
	s.change(w, r, http.MethodPost, "wifi/ap/"+strconv.Itoa(id)+"/neighbors/scan", nil,
		"Analyse lancée : la radio est indisponible un instant.", "/wifi-radar?ap="+strconv.Itoa(id))
}

// radarForm reads the radio id of a radar form.
func radarForm(w http.ResponseWriter, r *http.Request) (int, bool) {
	id := formInt(r, "ap")
	if id < 0 || r.PostFormValue("ap") == "" {
		setFlash(w, "err:Radio invalide.")
		http.Redirect(w, r, "/wifi-radar", http.StatusSeeOther)
		return 0, false
	}
	return id, true
}
