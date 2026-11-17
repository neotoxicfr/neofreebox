package server

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Switch: the box's Ethernet ports (link, hosts, forced speed/duplex, counters).

func init() {
	register(module{Slug: "switch", Title: "Ports Ethernet", Group: "Réseau", Order: 50, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /switch", s.switchPage)
		s.page(mux, "POST /switch/{id}", s.switchPortUpdate)
	}})
	id := ops.Param{Name: "id", Description: "Switch port id (1 to 4)"}
	ops.Register(
		ops.Op{Name: "switch_status_get", Description: "List the Ethernet ports: id, name, link (up/down), speed, duplex, mode, mac_list (hosts seen on the port).",
			Path: "switch/status/"},
		ops.Op{Name: "switch_port_get", Description: "Get a port configuration: speed (auto/10/100/1000) and duplex (auto/half/full).",
			Path: "switch/port/{id}", Params: []ops.Param{id}},
		ops.Op{Name: "switch_port_update", Description: "Change a port speed and/or duplex. Forcing a mode the device at the other end does not support drops the link.",
			Method: http.MethodPut, Path: "switch/port/{id}", Params: []ops.Param{id}, Body: true, Dangerous: true,
			BodyHelp: `{"speed": "1000", "duplex": "auto"}`},
		ops.Op{Name: "switch_port_stats_get", Description: "Get a port counters: rx/tx bytes, packets, rates (bytes/s and packets/s), errors, collisions.",
			Path: "switch/port/{id}/stats", Params: []ops.Param{id}},
	)
}

type switchPort struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Link    string `json:"link"`
	Speed   string `json:"speed"`
	Duplex  string `json:"duplex"`
	Mode    string `json:"mode"`
	MACList []struct {
		MAC      string `json:"mac"`
		Hostname string `json:"hostname"`
	} `json:"mac_list"`
	// Config is the forced speed/duplex ("auto" by default), read apart.
	Config struct {
		Speed  string `json:"speed"`
		Duplex string `json:"duplex"`
	} `json:"-"`
	Stats *switchStats `json:"-"`
}

type switchStats struct {
	RxBytes      int64 `json:"rx_good_bytes"`
	TxBytes      int64 `json:"tx_bytes"`
	RxRate       int64 `json:"rx_bytes_rate"`
	TxRate       int64 `json:"tx_bytes_rate"`
	RxPackets    int64 `json:"rx_good_packets"`
	TxPackets    int64 `json:"tx_packets"`
	RxErr        int64 `json:"rx_err_packets"`
	RxFCS        int64 `json:"rx_fcs_packets"`
	TxCollisions int64 `json:"tx_collisions"`
}

// Errors is the sum of the counters that point at a bad cable or a mismatch.
func (st switchStats) Errors() int64 { return st.RxErr + st.RxFCS + st.TxCollisions }

// Title is the port's name, or a default built from its id.
func (p switchPort) Title() string {
	if p.Name != "" {
		return p.Name
	}
	return "Ethernet " + strconv.Itoa(p.ID)
}

func (s *Server) switchPage(w http.ResponseWriter, r *http.Request) {
	var ports []switchPort
	err := s.fetch(r.Context(), "switch/status/", &ports)
	sort.Slice(ports, func(i, j int) bool { return ports[i].ID < ports[j].ID })
	for i := range ports {
		p := &ports[i]
		base := "switch/port/" + strconv.Itoa(p.ID)
		if s.fetch(r.Context(), base, &p.Config) != nil { // optional detail
			p.Config.Speed, p.Config.Duplex = "auto", "auto"
		}
		var st switchStats
		if s.fetch(r.Context(), base+"/stats", &st) == nil {
			p.Stats = &st
		}
	}
	s.show(w, r, "switch", map[string]any{"Ports": ports}, err)
}

var (
	switchSpeeds  = map[string]bool{"auto": true, "10": true, "100": true, "1000": true}
	switchDuplexs = map[string]bool{"auto": true, "half": true, "full": true}
)

// switchPortUpdate forces a port's speed and duplex; a wrong choice drops the link.
func (s *Server) switchPortUpdate(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/switch") {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	speed, duplex := formStr(r, "speed"), formStr(r, "duplex")
	if err != nil || id < 1 || !switchSpeeds[speed] || !switchDuplexs[duplex] {
		setFlash(w, "err:Port, vitesse ou duplex invalide.")
		http.Redirect(w, r, "/switch", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "switch/port/"+strconv.Itoa(id), map[string]string{"speed": speed, "duplex": duplex},
		"Port enregistré.", "/switch")
}
