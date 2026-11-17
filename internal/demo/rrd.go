package demo

import (
	"encoding/json"
	"math"
	"strconv"
)

// rrd answers a POST rrd/: about 200 points between date_start and date_end,
// each a sine plus a deterministic noise (the same request draws the same
// curves).
func (b *Box) rrd(in any) M {
	var req struct {
		DB        string `json:"db"`
		DateStart int64  `json:"date_start"`
		DateEnd   int64  `json:"date_end"`
	}
	raw, _ := json.Marshal(in)
	_ = json.Unmarshal(raw, &req)
	end := req.DateEnd
	if end == 0 {
		end = b.Now().Unix()
	}
	start := req.DateStart
	if start == 0 || start >= end {
		start = end - 86400
	}
	step := max((end-start)/200, 10)
	data := L{}
	for t := start - start%step; t <= end; t += step {
		p := M{"time": t}
		day := float64(t%86400) / 86400 * 2 * math.Pi
		switch req.DB {
		case "net":
			evening := math.Max(0, math.Sin(day-math.Pi*0.7)) // peak in the evening
			p["rate_down"] = 150_000 + 3_000_000*evening*(0.7+0.3*noise(t, 1)) + 80_000*noise(t, 2)
			p["rate_up"] = 20_000 + 260_000*evening*(0.6+0.4*noise(t, 3)) + 10_000*noise(t, 4)
			p["bw_down"], p["bw_up"] = 125_000_000, 75_000_000
		case "temp":
			p["cpum"] = 50 + 4*math.Sin(day) + 2*noise(t, 5)
			p["cpub"] = 48 + 3*math.Sin(day) + 2*noise(t, 6)
			p["sw"] = 46 + 2*math.Sin(day) + noise(t, 7)
			p["hdd"] = 37 + math.Sin(day) + noise(t, 8)
			p["fan0_speed"] = 1400 + 120*math.Sin(day) + 30*noise(t, 9)
		case "switch":
			for i := 1; i <= 4; i++ {
				on := 0.5 + 0.5*math.Sin(day+float64(i))
				if i == 4 {
					on = 0 // nothing plugged in port 4
				}
				p["rx_"+strconv.Itoa(i)] = 400_000 * on * float64(i) * (0.5 + noise(t, int64(10+i)))
				p["tx_"+strconv.Itoa(i)] = 60_000 * on * float64(i) * (0.5 + noise(t, int64(20+i)))
			}
		}
		data = append(data, p)
	}
	return M{"date_start": start, "date_end": end, "data": data}
}

// noise is a deterministic value in [0, 1) for a timestamp and a series.
func noise(t int64, seed int64) float64 {
	x := uint64(t*2654435761 + seed*40503)
	x ^= x >> 13
	x *= 0x9E3779B97F4A7C15
	x ^= x >> 29
	return float64(x%10000) / 10000
}
