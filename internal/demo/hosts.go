package demo

import "strconv"

// M and L are the JSON objects and arrays of the answers.
type (
	M = map[string]any
	L = []any
)

// device is one machine of the fictional household. The same list feeds the
// device page, the DHCP leases, the redirections, the Wi-Fi stations and the
// parental control, so every page tells the same story.
type device struct {
	name   string
	mac    string // locally administered (02:...), so it never belongs to a vendor
	ip     string
	typ    string // host_type
	wifi   bool
	static bool // has a static DHCP lease
	active bool
	ago    int64 // seconds since last activity when inactive
}

const (
	boxIP    = "192.168.1.254"
	publicIP = "192.0.2.17"
	prefix6  = "2001:db8:4a1c:7700::"
)

var devices = []device{
	{"Freebox Player", "02:00:00:10:00:01", "192.168.1.20", "freebox_player", false, true, true, 0},
	{"TV du salon", "02:00:00:10:00:02", "192.168.1.21", "television", false, false, true, 0},
	{"NAS maison", "02:00:00:10:00:03", "192.168.1.30", "nas", false, true, true, 0},
	{"Imprimante bureau", "02:00:00:10:00:04", "192.168.1.40", "printer", true, true, true, 0},
	{"PC de Marc", "02:00:00:10:00:05", "192.168.1.50", "workstation", false, true, true, 0},
	{"Portable de Sophie", "02:00:00:10:00:06", "192.168.1.51", "laptop", true, false, true, 0},
	{"Téléphone de Marc", "02:00:00:10:00:07", "192.168.1.60", "smartphone", true, false, true, 0},
	{"Téléphone de Sophie", "02:00:00:10:00:08", "192.168.1.61", "smartphone", true, false, true, 0},
	{"Tablette de Lucas", "02:00:00:10:00:09", "192.168.1.62", "tablet", true, false, true, 0},
	{"Console de Lucas", "02:00:00:10:00:0a", "192.168.1.70", "vg_console", true, false, false, 5400},
	{"Caméra entrée", "02:00:00:10:00:0b", "192.168.1.80", "ip_camera", true, true, true, 0},
	{"Thermostat", "02:00:00:10:00:0c", "192.168.1.81", "iot_device", true, false, true, 0},
	{"Enceinte cuisine", "02:00:00:10:00:0d", "192.168.1.82", "iot_device", true, false, true, 0},
}

func (d device) ipv6() string { return prefix6 + strconv.FormatInt(int64(d.index()), 16) }

func (d device) index() int {
	for i, x := range devices {
		if x.mac == d.mac {
			return i + 0x10
		}
	}
	return 0
}

func (d device) apType() string {
	if d.wifi {
		return "wifi"
	}
	return "gateway"
}

// host is the lan/browser entry of a device, with every field the pages read.
func (d device) host(now int64) M {
	last := now
	if !d.active {
		last = now - d.ago
	}
	l3 := L{
		M{"addr": d.ip, "af": "ipv4", "active": d.active, "reachable": d.active, "last_activity": last, "last_time_reachable": last},
		M{"addr": d.ipv6(), "af": "ipv6", "active": d.active, "reachable": d.active, "last_activity": last, "last_time_reachable": last},
	}
	ap := M{"mac": "02:00:00:00:00:01", "type": d.apType(), "uid": "box", "connectivity_type": d.apType(), "rx_bytes": 48_000_000 + int64(d.index())*7_000_000, "tx_bytes": 920_000_000 + int64(d.index())*31_000_000}
	if d.wifi {
		ap["wifi_information"] = M{"band": "5G", "ssid": "Maison", "signal": -52 - d.index(), "bssid": "02:00:00:00:01:01"}
	} else {
		ap["ethernet_information"] = M{"duplex": "full", "speed": 1000, "max_port_speed": 2500, "link": "up"}
	}
	return M{
		"id": "ether-" + d.mac, "l2ident": M{"id": d.mac, "type": "mac_address"},
		"primary_name": d.name, "default_name": d.name, "primary_name_manual": true,
		"names":     L{M{"name": d.name, "source": "dhcp"}},
		"host_type": d.typ, "vendor_name": "", "active": d.active, "reachable": d.active, "persistent": true,
		"interface": "pub", "local_domain": "freebox.local",
		"first_activity": now - 86400*120, "last_activity": last, "last_time_reachable": last,
		"l3connectivities": l3, "access_point": ap,
	}
}

func (b *Box) hosts(now int64) L {
	out := make(L, 0, len(devices))
	for _, d := range devices {
		out = append(out, d.host(now))
	}
	return out
}

func (b *Box) staticLeases(now int64) L {
	out := L{}
	for _, d := range devices {
		if d.static {
			out = append(out, M{"id": d.mac, "mac": d.mac, "ip": d.ip, "hostname": toHostname(d.name),
				"comment": d.name, "host": d.host(now), "options": M{}})
		}
	}
	return out
}

func (b *Box) dynamicLeases(now int64) L {
	out := L{}
	for _, d := range devices {
		if !d.active {
			continue
		}
		out = append(out, M{"mac": d.mac, "ip": d.ip, "hostname": toHostname(d.name), "is_static": d.static,
			"assign_time": now - 3600*int64(d.index()), "refresh_time": now - 600*int64(d.index()-0xf), "lease_remaining": 86400 - 600*int64(d.index()-0xf),
			"host": d.host(now)})
	}
	return out
}

// toHostname makes a DNS-like name from a display name.
func toHostname(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, byte(r))
		case r >= 'A' && r <= 'Z':
			out = append(out, byte(r+32))
		case r == 'é' || r == 'è':
			out = append(out, 'e')
		case r == ' ':
			out = append(out, '-')
		}
	}
	return string(out)
}
