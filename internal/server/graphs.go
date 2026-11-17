package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Graphs: the box's RRD statistics (network, temperatures, switch ports)
// drawn as inline SVG on the server, so the page needs no script.

func init() {
	register(module{Slug: "graphs", Title: "Historiques", Group: "Supervision", Order: 0, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /graphs", s.graphs)
	}})
	ops.Register(
		// The statistics are read with a POST (the request has a body) but nothing is changed.
		ops.Op{Name: "rrd_fetch", Method: http.MethodPost, Path: "rrd/",
			Description: "Read RRD statistics: db = net (rate_up, rate_down, bw_up, bw_down in byte/s), temp (temperatures in °C, fan speed), switch (rx_N and tx_N per port in byte/s) or dsl. Returns date_start, date_end and data[] (one object per time step with a \"time\" field). Nothing is modified.",
			Body:        true, BodyHelp: `{"db": "net", "date_start": 1794900000, "date_end": 1794986400, "fields": ["rate_up", "rate_down"], "precision": 10}; date_start, date_end, fields and precision are optional (precision multiplies every value, divide the result)`},
		ops.Op{Name: "rrd_read", Path: "rrd/",
			Description: "Same as rrd_fetch with query parameters, allowed without the settings permission: RRD statistics of one database (net, temp, dsl, switch).",
			Params: []ops.Param{
				{Name: "db", Required: true, Query: true, Description: "net, temp, dsl or switch"},
				{Name: "date_start", Type: "integer", Query: true, Description: "Start, unix timestamp (optional)"},
				{Name: "date_end", Type: "integer", Query: true, Description: "End, unix timestamp (optional)"},
				{Name: "precision", Type: "integer", Query: true, Description: "Multiplies every value; divide the result (optional)"},
			}},
	)
}

// graphRanges are the periods offered, in the order of the buttons.
var graphRanges = []struct {
	Key, Label string
	Span       time.Duration
	Day        bool // x labels are dates, not hours
}{
	{"1h", "1 h", time.Hour, false},
	{"24h", "24 h", 24 * time.Hour, false},
	{"7j", "7 j", 7 * 24 * time.Hour, true},
	{"30j", "30 j", 30 * 24 * time.Hour, true},
}

type rrdPoint map[string]*float64

type rrdResult struct {
	Data []rrdPoint `json:"data"`
}

// seriesDef picks one RRD field and says how to draw it.
type seriesDef struct{ Key, Name, Color string }

type tick struct {
	Pos   float64
	Label string
}

type chartSeries struct {
	Name, Color, Dash, Points, Last, Max string
}

type chart struct {
	Title       string
	W, H        int
	Left, Right float64 // x of the plot area edges
	Top, Bottom float64
	Series      []chartSeries
	YTicks      []tick
	XTicks      []tick
	Empty       bool
	Desc        string // text alternative
}

const (
	chartW, chartH = 400, 200
	maxPoints      = 240
	colorUp        = "#c2610c"
	colorDown      = "#2a7fd0"
)

// palette and dashes: every colour has at least 3:1 against the white and the
// dark card backgrounds, and the lines also differ by their dash pattern.
var (
	palette = []string{"#0e8a8a", "#2a7fd0", "#b26a00", "#178a3a", "#8b5cf6", "#ec4899"}
	dashes  = []string{"", "7 3", "2 3", "9 3 2 3", "12 4", "1 4"}
)

func (s *Server) graphs(w http.ResponseWriter, r *http.Request) {
	rng := graphRanges[1]
	for _, g := range graphRanges {
		if g.Key == r.URL.Query().Get("range") {
			rng = g
		}
	}
	end := time.Now().Unix()
	start := end - int64(rng.Span/time.Second)

	dbs := []string{"net", "temp", "switch"}
	results := make([]rrdResult, len(dbs))
	failed := make([]error, len(dbs))
	var g errgroup.Group
	for i, db := range dbs {
		g.Go(func() error {
			body := map[string]any{"db": db, "date_start": start, "date_end": end}
			failed[i] = s.boxDo(r, http.MethodPost, "rrd/", body, &results[i])
			return nil
		})
	}
	_ = g.Wait()

	var charts []chart
	var errs []string
	for i, db := range dbs {
		if failed[i] != nil {
			errs = append(errs, fmt.Sprintf("%s : %v", dbLabel(db), failed[i]))
			continue
		}
		pts := downsample(results[i].Data, maxPoints)
		switch db {
		case "net":
			charts = append(charts, makeChart("Débit", pts, start, end, rng.Day, 8, bits64,
				[]seriesDef{{"rate_down", "Descendant", colorDown}, {"rate_up", "Montant", colorUp}}))
		case "temp":
			temps, fans := splitTemp(pts)
			charts = append(charts,
				makeChart("Températures", pts, start, end, rng.Day, 1, func(v float64) string { return fmt.Sprintf("%.0f °C", v) }, temps),
				makeChart("Ventilateur", pts, start, end, rng.Day, 1, func(v float64) string { return fmt.Sprintf("%.0f tr/min", v) }, fans))
		case "switch":
			for _, p := range switchPorts(pts) {
				charts = append(charts, makeChart("Switch, port "+p, pts, start, end, rng.Day, 8, bits64,
					[]seriesDef{{"rx_" + p, "Reçu", colorDown}, {"tx_" + p, "Émis", colorUp}}))
			}
		}
	}
	var err error
	if len(errs) == 3 {
		err = fmt.Errorf("%s", strings.Join(errs, " ; "))
	}
	s.show(w, r, "graphs", map[string]any{"Charts": charts, "Errors": errs, "Range": rng.Key, "Ranges": graphRanges}, err)
}

func dbLabel(db string) string {
	switch db {
	case "net":
		return "débit"
	case "temp":
		return "températures"
	}
	return "switch"
}

// boxDo is Box.Do with the 15 s timeout of the read helpers.
func (s *Server) boxDo(r *http.Request, method, path string, in, out any) error {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	return s.box.Do(ctx, method, path, in, out)
}

// bits64 formats a rate given in bit/s.
func bits64(v float64) string { return bits(int64(v)) }

// downsample averages consecutive points so a long period stays light.
func downsample(in []rrdPoint, n int) []rrdPoint {
	if len(in) <= n {
		return in
	}
	step := (len(in) + n - 1) / n
	var out []rrdPoint
	for i := 0; i < len(in); i += step {
		chunk := in[i:min(i+step, len(in))]
		sum, cnt := map[string]float64{}, map[string]int{}
		for _, p := range chunk {
			for k, v := range p {
				if v != nil {
					sum[k] += *v
					cnt[k]++
				}
			}
		}
		o := rrdPoint{}
		for k, c := range cnt {
			v := sum[k] / float64(c)
			if k == "time" {
				v = *chunk[len(chunk)-1]["time"]
			}
			o[k] = &v
		}
		out = append(out, o)
	}
	return out
}

// splitTemp separates the temperature fields from the fan speeds.
func splitTemp(pts []rrdPoint) (temps, fans []seriesDef) {
	names := map[string]string{"cpum": "CPU M", "cpub": "CPU B", "sw": "Switch", "hdd": "Disque",
		"temp_cpub": "CPU B", "temp_cpum": "CPU M", "temp_sw": "Switch", "temp_hdd": "Disque",
		"temp_t1": "Température 1", "temp_t2": "Température 2", "temp_t3": "Température 3"}
	for _, k := range fieldKeys(pts) {
		name := names[k]
		if name == "" {
			name = k
		}
		if strings.Contains(k, "fan") {
			fans = append(fans, seriesDef{k, name, palette[len(fans)%len(palette)]})
		} else {
			temps = append(temps, seriesDef{k, name, palette[len(temps)%len(palette)]})
		}
	}
	// The box numbers its fans from 0 in the RRD and from 1 on the State page.
	for i := range fans {
		n := strings.Trim(strings.NewReplacer("fan", "", "_speed", "").Replace(fans[i].Key), "_")
		if v, err := strconv.Atoi(n); err == nil {
			n = strconv.Itoa(v + 1)
		}
		fans[i].Name = "Ventilateur " + n
	}
	return temps, fans
}

// switchPorts lists the port numbers that have a rx_N field.
func switchPorts(pts []rrdPoint) []string {
	var ports []string
	for _, k := range fieldKeys(pts) {
		if p, ok := strings.CutPrefix(k, "rx_"); ok {
			ports = append(ports, p)
		}
	}
	return ports
}

// fieldKeys is the sorted set of fields (but "time") found in the points.
func fieldKeys(pts []rrdPoint) []string {
	seen := map[string]bool{}
	for _, p := range pts {
		for k := range p {
			if k != "time" {
				seen[k] = true
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// niceMax rounds v up to 1, 2 or 5 times a power of ten.
func niceMax(v float64) float64 {
	if v <= 0 {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 5, 10} {
		if v <= m*p {
			return m * p
		}
	}
	return 10 * p
}

// makeChart draws the series as polylines. mult converts the RRD value to
// the displayed unit (byte/s to bit/s: 8).
func makeChart(title string, pts []rrdPoint, from, to int64, days bool, mult float64, format func(float64) string, defs []seriesDef) chart {
	c := chart{Title: title, W: chartW, H: chartH, Left: 84, Right: chartW - 24, Top: 10, Bottom: 26}
	pw, ph := c.Right-c.Left, float64(c.H)-c.Top-c.Bottom
	maxV := 0.0
	for _, p := range pts {
		for _, d := range defs {
			if v := p[d.Key]; v != nil {
				maxV = math.Max(maxV, *v*mult)
			}
		}
	}
	top := niceMax(maxV)
	span := float64(max(to-from, 1))
	for i, d := range defs {
		var poly []string
		last, peak, seen := 0.0, 0.0, false
		for _, p := range pts {
			v, t := p[d.Key], p["time"]
			if v == nil || t == nil {
				continue
			}
			x := c.Left + (*t-float64(from))/span*pw
			if x < c.Left || x > c.Left+pw {
				continue
			}
			y := c.Top + ph - *v*mult/top*ph
			poly = append(poly, fmt.Sprintf("%.1f,%.1f", x, y))
			last, peak, seen = *v*mult, math.Max(peak, *v*mult), true
		}
		if !seen {
			continue
		}
		c.Series = append(c.Series, chartSeries{Name: d.Name, Color: d.Color, Dash: dashes[i%len(dashes)], Points: strings.Join(poly, " "),
			Last: format(last), Max: format(peak)})
	}
	c.Empty = len(c.Series) == 0
	for i := 0; i <= 4; i++ {
		f := float64(i) / 4
		c.YTicks = append(c.YTicks, tick{c.Top + ph - f*ph, format(top * f)})
	}
	layout := "15:04"
	if days {
		layout = "02/01"
	}
	for i := 0; i <= 4; i++ {
		f := float64(i) / 4
		c.XTicks = append(c.XTicks, tick{c.Left + f*pw, time.Unix(from+int64(f*span), 0).Format(layout)})
	}
	var parts []string
	for _, s := range c.Series {
		parts = append(parts, fmt.Sprintf("%s : dernière valeur %s, maximum %s", s.Name, s.Last, s.Max))
	}
	c.Desc = title + ". " + strings.Join(parts, ". ")
	return c
}
