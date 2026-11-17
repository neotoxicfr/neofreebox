package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
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

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var (
		sys   systemInfo
		conn  connection
		perms map[string]bool
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return s.box.Get(gctx, "system/", &sys) })
	g.Go(func() error { return s.box.Get(gctx, "connection/", &conn) })
	g.Go(func() (err error) { perms, err = s.box.Permissions(gctx); return err })
	data := map[string]any{"Nav": "status"}
	if err := g.Wait(); err != nil {
		s.log.Error("status", "err", err)
		data["Error"] = "La box ne répond pas : " + err.Error()
		s.render(w, http.StatusBadGateway, "status", data)
		return
	}
	granted := make([]string, 0, len(perms))
	for k, v := range perms {
		if v {
			granted = append(granted, k)
		}
	}
	sort.Strings(granted)
	data["System"], data["Conn"], data["Rights"] = sys, conn, granted
	data["Uptime"] = duration(time.Duration(sys.UptimeVal) * time.Second)
	data["Down"], data["Up"] = bits(conn.RateDown*8), bits(conn.RateUp*8)
	data["MaxDown"], data["MaxUp"] = bits(conn.BandwidthDown), bits(conn.BandwidthUp)
	s.render(w, http.StatusOK, "status", data)
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
