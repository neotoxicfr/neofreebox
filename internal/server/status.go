package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

type systemInfo struct {
	FirmwareVersion string `json:"firmware_version"`
	UptimeVal       int64  `json:"uptime_val"`
	ModelInfo       struct {
		PrettyName string `json:"pretty_name"`
	} `json:"model_info"`
	Sensors []reading `json:"sensors"`
	Fans    []reading `json:"fans"`
}

type reading struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// StateLabel is the line state in French.
func (c connection) StateLabel() string { return lineState(c.State) }

// MediaLabel is the line type in French, as on the connection page.
func (c connection) MediaLabel() string { return connStatus{Media: c.Media}.MediaLabel() }

type connection struct {
	State         string `json:"state"`
	Media         string `json:"media"`
	IPv4          string `json:"ipv4"`
	IPv6          string `json:"ipv6"`
	RateUp        int64  `json:"rate_up"`        // byte/s
	RateDown      int64  `json:"rate_down"`      // byte/s
	BandwidthUp   int64  `json:"bandwidth_up"`   // bit/s
	BandwidthDown int64  `json:"bandwidth_down"` // bit/s
}

func init() {
	register(module{Slug: "status", Title: "État", Group: "Freebox", Order: 0, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /{$}", s.status)
	}})
}

// statusData is what the home page and /api/v1/summary show. The optional
// figures are nil when their endpoint failed or is not allowed.
type statusData struct {
	System systemInfo
	Conn   connection
	Rights []string
	Active *int // connected devices
	Total  *int // devices seen by the box
	Unread *int // unread calls
	WifiOn *bool
}

// collectStatus reads the box for the home page. system/ and connection/ are
// required; the other figures are optional and get a short leash, so a slow
// or forbidden one only drops its line.
func (s *Server) collectStatus(ctx context.Context) (statusData, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var (
		d     statusData
		perms map[string]bool
		hosts []lanHost
		calls []struct {
			New bool `json:"new"`
		}
		wifi struct {
			Enabled bool `json:"enabled"`
		}
		haveHost, haveCall, haveWifi bool
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return s.box.Get(gctx, "system/", &d.System) })
	g.Go(func() error { return s.box.Get(gctx, "connection/", &d.Conn) })
	g.Go(func() (err error) { perms, err = s.box.Permissions(gctx); return err })
	soft := func(ok *bool, path string, out any) {
		g.Go(func() error {
			octx, cancel := context.WithTimeout(gctx, 3*time.Second)
			defer cancel()
			*ok = s.box.Get(octx, path, out) == nil
			return nil
		})
	}
	soft(&haveHost, "lan/browser/pub/", &hosts)
	soft(&haveCall, "call/log/", &calls)
	soft(&haveWifi, "wifi/config/", &wifi)
	if err := g.Wait(); err != nil {
		return d, err
	}
	for k, v := range perms {
		if v {
			d.Rights = append(d.Rights, k)
		}
	}
	sort.Strings(d.Rights)
	if haveHost {
		active := 0
		for _, h := range hosts {
			if h.Active {
				active++
			}
		}
		total := len(hosts)
		d.Active, d.Total = &active, &total
	}
	if haveCall {
		unread := 0
		for _, c := range calls {
			if c.New {
				unread++
			}
		}
		d.Unread = &unread
	}
	if haveWifi {
		d.WifiOn = &wifi.Enabled
	}
	for i := range d.System.Fans { // same names as the graphs page: "Ventilateur 1"…
		d.System.Fans[i].Name = fmt.Sprintf("Ventilateur %d", i+1)
	}
	return d, nil
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"Nav": "status"}
	d, err := s.collectStatus(r.Context())
	if err != nil {
		s.log.Error("status", "err", err)
		data["Error"] = "La box ne répond pas : " + err.Error()
		s.render(w, http.StatusBadGateway, "status", data)
		return
	}
	data["System"], data["Conn"], data["Rights"] = d.System, d.Conn, d.Rights
	brief := map[string]any{}
	if d.Active != nil {
		brief["HasHosts"], brief["Active"], brief["Total"] = true, *d.Active, *d.Total
	}
	if d.Unread != nil {
		brief["HasCalls"], brief["Unread"] = true, *d.Unread
	}
	if d.WifiOn != nil {
		brief["HasWifi"], brief["Wifi"] = true, *d.WifiOn
	}
	data["Brief"] = brief
	data["Uptime"] = duration(time.Duration(d.System.UptimeVal) * time.Second)
	data["Down"], data["Up"] = bits(d.Conn.RateDown*8), bits(d.Conn.RateUp*8)
	data["MaxDown"], data["MaxUp"] = bits(d.Conn.BandwidthDown), bits(d.Conn.BandwidthUp)
	s.render(w, http.StatusOK, "status", data)
}

// apiSummary is a read-only digest for dashboards: figures and states only,
// no address, MAC or device name. Any API token may read it.
func (s *Server) apiSummary(w http.ResponseWriter, r *http.Request) {
	d, err := s.collectStatus(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": humanError(err)})
		return
	}
	type temp struct {
		Name    string `json:"name"`
		Celsius int    `json:"celsius"`
	}
	out := map[string]any{
		"ok": true,
		"connection": map[string]any{
			"up": d.Conn.State == "up", "state": d.Conn.State, "media": d.Conn.Media,
			"rate_down_bps": d.Conn.RateDown * 8, "rate_up_bps": d.Conn.RateUp * 8,
			"bandwidth_down_bps": d.Conn.BandwidthDown, "bandwidth_up_bps": d.Conn.BandwidthUp,
		},
		"box": map[string]any{
			"model": d.System.ModelInfo.PrettyName, "firmware": d.System.FirmwareVersion,
			"uptime_s": d.System.UptimeVal,
		},
	}
	temps := []temp{}
	maxTemp := 0
	for _, t := range d.System.Sensors {
		temps = append(temps, temp{t.Name, t.Value})
		maxTemp = max(maxTemp, t.Value)
	}
	out["temperatures"], out["temperature_max_c"] = temps, maxTemp
	fans := []int{}
	for _, f := range d.System.Fans {
		fans = append(fans, f.Value)
	}
	out["fans_rpm"] = fans
	if d.Active != nil {
		out["devices"] = map[string]int{"connected": *d.Active, "seen": *d.Total}
	}
	if d.WifiOn != nil {
		out["wifi"] = map[string]bool{"enabled": *d.WifiOn}
	}
	if d.Unread != nil {
		out["calls"] = map[string]int{"unread": *d.Unread}
	}
	writeJSON(w, http.StatusOK, out)
}

// bits formats a rate in bit/s, in French units.
func bits(v int64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.2f Gb/s", float64(v)/1e9)
	case v >= 100e6:
		return fmt.Sprintf("%.0f Mb/s", float64(v)/1e6)
	case v >= 1e6:
		return fmt.Sprintf("%.1f Mb/s", float64(v)/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.0f kb/s", float64(v)/1e3)
	}
	return fmt.Sprintf("%d b/s", v)
}

// byteSize formats a size in bytes, in French units (Ko, Mo…).
func byteSize(v int64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d o", v)
	}
	div, exp := int64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %co", float64(v)/float64(div), "KMGTPE"[exp])
}

func duration(d time.Duration) string {
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%d j %d h", days, h)
	}
	return fmt.Sprintf("%d h %02d min", h, m)
}

// shortDuration writes a duration the way people say it: "45 s", "12 min",
// "2 h 05", "3 j 4 h".
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h %02d", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%d j %d h", int(d.Hours())/24, int(d.Hours())%24)
}

// band names a Wi-Fi band the same way everywhere: "2,4 GHz", "5 GHz"…
func band(b string) string {
	switch strings.ToLower(strings.TrimSpace(b)) {
	case "2g4", "2.4g", "2d4g", "2.4ghz", "2,4 ghz":
		return "2,4 GHz"
	case "5g", "5ghz":
		return "5 GHz"
	case "6g", "6ghz":
		return "6 GHz"
	case "60g", "60ghz":
		return "60 GHz"
	}
	return b
}
