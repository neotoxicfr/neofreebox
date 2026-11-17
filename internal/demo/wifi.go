package demo

import "strconv"

var apBands = []string{"2d4g", "5g", "6g"}

var apChannels = map[string][]int{"2d4g": {1, 6, 11}, "5g": {36, 40, 44, 48}, "6g": {37, 41, 45}}

// onBand says on which radio a Wi-Fi device sits: the small connected objects
// stay on 2.4 GHz.
func onBand(d device) int {
	switch d.typ {
	case "printer", "ip_camera", "iot_device":
		return 0
	}
	return 1
}

func (b *Box) apSub(id int, sub string, now int64) (any, bool) {
	if id < 0 || id >= len(apBands) {
		return nil, false
	}
	band := apBands[id]
	width := []int{20, 80, 160}[id]
	switch sub {
	case "":
		return b.ap(id, band, width, []int{6, 36, 37}[id], 0), true
	case "stations":
		out := L{}
		for i, d := range devices {
			if !d.wifi || !d.active || onBand(d) != id {
				continue
			}
			rate := 400_000_000 - int64(i)*20_000_000
			out = append(out, M{
				"id": d.mac, "mac": d.mac, "hostname": toHostname(d.name), "bssid": "02:00:00:00:01:01", "state": "authenticated",
				"signal": -48 - i*2, "inactive": int64(i % 4), "conn_duration": 3600*int64(i+1) + 120,
				"rx_bytes": 90_000_000 + int64(i)*13_000_000, "tx_bytes": 1_300_000_000 + int64(i)*41_000_000, "rx_rate": rate / 8, "tx_rate": rate / 10,
				"last_tx": M{"bitrate": rate / 1_000_000 / 2, "width": "80", "mcs": 9}, "last_rx": M{"bitrate": rate / 1_000_000 / 3, "width": "80", "mcs": 8},
				"host": d.host(now),
			})
		}
		return out, true
	case "neighbors":
		ch := apChannels[band][0]
		return L{
			M{"ssid": "VoisinWifi-A1", "bssid": "02:00:00:30:00:01", "band": band, "channel": ch, "secondary_channel": 0, "channel_width": "20", "signal": -61},
			M{"ssid": "Réseau-du-4e", "bssid": "02:00:00:30:00:02", "band": band, "channel": ch, "secondary_channel": 0, "channel_width": "20", "signal": -72},
			M{"ssid": "Café-Invités", "bssid": "02:00:00:30:00:03", "band": band, "channel": apChannels[band][1], "secondary_channel": 0, "channel_width": "20", "signal": -80},
		}, true
	case "channel_usage":
		out := L{}
		for i, c := range apChannels[band] {
			out = append(out, M{"band": band, "channel": c, "noise_level": -92 + i, "rx_busy_percent": 8 + i*7})
		}
		return out, true
	case "allowed_channel_comb":
		out := L{}
		for _, c := range apChannels[band] {
			out = append(out, M{"band": band, "channel_width": strconv.Itoa(width), "primary": c, "secondary": 0, "need_dfs": false, "dfs_cac_time": 0, "psc": false})
		}
		return out, true
	}
	return nil, false
}
