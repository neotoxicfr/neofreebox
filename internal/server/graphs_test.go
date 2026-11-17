package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type graphsBox struct {
	fakeBox
	mu     sync.Mutex
	bodies []map[string]any
}

func (b *graphsBox) Do(_ context.Context, method, path string, in, out any) error {
	if method != http.MethodPost || path != "rrd/" {
		return errors.New("unexpected " + method + " " + path)
	}
	req := in.(map[string]any)
	b.mu.Lock()
	b.bodies = append(b.bodies, req)
	b.mu.Unlock()
	start := req["date_start"].(int64)
	var pts []string
	for i := range int64(10) {
		t := start + i*60
		switch req["db"] {
		case "net":
			pts = append(pts, fmt.Sprintf(`{"time":%d,"rate_up":%d,"rate_down":%d,"bw_up":1,"bw_down":1}`, t, 125000*(i+1), 250000))
		case "temp":
			pts = append(pts, fmt.Sprintf(`{"time":%d,"temp_cpub":%d,"temp_t1":50,"fan0_speed":1500}`, t, 60+i))
		case "switch":
			pts = append(pts, fmt.Sprintf(`{"time":%d,"rx_1":1000,"tx_1":null,"rx_2":2000,"tx_2":500}`, t))
		}
	}
	return json.Unmarshal([]byte(`{"data":[`+strings.Join(pts, ",")+`]}`), out)
}

func TestGraphsPage(t *testing.T) {
	box := &graphsBox{}
	srv := newTestServer(t, box)
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	for _, rng := range []string{"1h", "24h", "7j", "30j", "bogus"} {
		resp, body := get(t, c, srv, "/graphs?range="+rng)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", rng, resp.StatusCode)
		}
		for _, want := range []string{"<polyline", "Descendant", "Montant", "Températures", "CPU B", "Ventilateur 1", "Température 1", "Switch, port 2", "10.0 Mb/s", "#e5383b"} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s: %q missing\n%s", rng, want, body)
			}
		}
		if strings.Contains(body, "style=") {
			t.Fatal("inline style")
		}
	}
	var net map[string]any
	for _, b := range box.bodies {
		if b["db"] == "net" {
			net = b
			break
		}
	}
	if net["date_end"].(int64)-net["date_start"].(int64) != 3600 {
		t.Fatalf("last request is not the 1 h default... got %v", net)
	}
}

func TestGraphsBoxDown(t *testing.T) {
	srv := newTestServer(t, fakeBox{})
	c := browser(srv)
	post(t, c, srv, "/setup", creds("admin", "correct horse battery"))
	// fakeBox answers a POST with {"done":true}: no data, no chart, but a page.
	if resp, body := get(t, c, srv, "/graphs"); resp.StatusCode != http.StatusOK || !strings.Contains(body, "Pas de données") {
		t.Fatalf("empty data: %d", resp.StatusCode)
	}
	srv2 := newTestServer(t, fakeBox{err: errors.New("timeout")})
	c2 := browser(srv2)
	post(t, c2, srv2, "/setup", creds("admin", "correct horse battery"))
	if resp, _ := get(t, c2, srv2, "/graphs"); resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("box down: %d", resp.StatusCode)
	}
}

func TestNiceMax(t *testing.T) {
	for in, want := range map[float64]float64{0: 1, 0.3: 0.5, 7: 10, 120: 200, 1e8: 1e8} {
		if got := niceMax(in); got != want {
			t.Errorf("niceMax(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestDownsample(t *testing.T) {
	var in []rrdPoint
	for i := range 1000 {
		v, tm := float64(i%2), float64(i)
		in = append(in, rrdPoint{"x": &v, "time": &tm})
	}
	out := downsample(in, 100)
	if len(out) != 100 || *out[0]["x"] != 0.5 || *out[99]["time"] != 999 {
		t.Fatalf("got %d points, first %v", len(out), *out[0]["x"])
	}
}
