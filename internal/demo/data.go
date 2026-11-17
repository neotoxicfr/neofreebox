package demo

import (
	"strconv"
	"strings"
)

// lookup returns the fictional answer of a GET, or false when the path is not
// one a page reads.
func (b *Box) lookup(path string) (any, bool) {
	now := b.Now().Unix()
	path = strings.TrimPrefix(path, "/")
	if i := strings.IndexByte(path, '?'); i >= 0 && !strings.HasPrefix(path, "fs/ls/") {
		path = path[:i]
	}
	if v, ok := b.static(path, now); ok {
		return v, true
	}
	return b.dynamic(path, now)
}

func (b *Box) static(path string, now int64) (any, bool) {
	switch path {
	case "system/":
		return M{
			"mac": "02:00:00:00:00:01", "serial": "DEMO0000001", "board_name": "fbxgw9r", "firmware_version": "4.9.99",
			"uptime": "12 jours, 4 heures", "uptime_val": 12*86400 + 4*3600 + 1260, "disk_status": "active",
			"usb3_enable": true, "user_storage_powered": true, "box_authenticated": true, "user_main_storage": "Disque dur",
			"model_info": M{"name": "fbxgw9r", "pretty_name": "Freebox Pop (démo)", "has_dsl": false, "has_lcd_orientation": true,
				"has_status_led": true, "led_strip_revision": 1, "wifi_type": "2d4_5g_6g", "default_language": "fr",
				"supported_languages": L{"fra", "eng", "ita"}, "internal_hdd_size": 0, "customer_hdd_slots": 1},
			"fans":    L{M{"id": "fan0_speed", "name": "Ventilateur 1", "value": 1430}},
			"sensors": L{M{"id": "temp_cpu", "name": "Température CPU", "value": 52}, M{"id": "temp_sw", "name": "Température Switch", "value": 47}, M{"id": "temp_hdd", "name": "Disque dur", "value": 38}},
		}, true
	case "update/":
		return M{"state": "idle", "upgrade_state": M{"state": "", "old_version": "", "new_version": "", "percent": 0}}, true
	case "lang/":
		return M{"lang": "fra", "avalaible": L{"fra", "eng", "ita"}}, true

	case "lan/browser/pub/":
		return b.hosts(now), true
	case "lan/config/":
		return M{"name": "demo-freebox", "name_dns": "demo-freebox", "name_mdns": "demo-freebox", "name_netbios": "DEMO-FREEBOX",
			"mode": "router", "ip": boxIP, "local_domain": "freebox.local"}, true
	case "lan/routes/":
		return L{M{"prefix": "10.20.0.0/24", "gateway": "192.168.1.30", "enabled": true, "description": "Réseau du laboratoire"}}, true

	case "dhcp/config/":
		return M{"enabled": true, "sticky_assign": true, "always_broadcast": false, "ignore_out_of_range_hint": false,
			"gateway": boxIP, "netmask": "255.255.255.0", "ip_range_start": "192.168.1.100", "ip_range_end": "192.168.1.199",
			"boot_server": "", "boot_file": "", "dns": L{"", "", "", "", "", ""}, "options": M{}}, true
	case "dhcp/static_lease/":
		return b.staticLeases(now), true
	case "dhcp/dynamic_lease/":
		return b.dynamicLeases(now), true
	case "dhcpv6/config/":
		return M{"enabled": true, "use_custom_dns": false, "dns": L{""}}, true

	case "connection/":
		return M{"state": "up", "type": "ethernet", "media": "ftth", "ipv4": publicIP, "ipv6": prefix6 + "1",
			"ipv4_port_range": L{0, 65535}, "rate_down": 2_350_000, "rate_up": 310_000, "bandwidth_down": 1_000_000_000, "bandwidth_up": 600_000_000,
			"bytes_down": int64(7_412_000_000_000), "bytes_up": int64(1_102_000_000_000)}, true
	case "connection/config/":
		return M{"ping": true, "wol": false, "adblock": true, "adblock_not_set": false, "allow_token_request": false, "sip_alg": "disabled",
			"remote_access": false, "remote_access_port": 8443, "remote_access_min_port": 8000, "remote_access_max_port": 9000,
			"remote_access_ip": publicIP, "is_secure_pass": true, "api_remote_access": false, "api_domain": "demo.example.net",
			"https_available": true, "https_port": 8443}, true
	case "connection/ipv6/config/":
		return M{"ipv6_enabled": true, "ipv6_firewall": true, "ipv6_prefix_firewall": false, "ipv6ll": "fe80::ff:fe00:1",
			"delegations": L{M{"prefix": prefix6 + "/64", "next_hop": "fe80::ff:fe00:2"}}}, true
	case "connection/ftth/":
		return M{"sfp_present": true, "sfp_alim_ok": true, "sfp_has_signal": true, "sfp_has_power_report": true, "link": true,
			"link_type": "ftth", "has_sfp": true, "sfp_model": "DEMO-XGS-PON", "sfp_vendor": "Fictif", "sfp_serial": "DEMO000001",
			"sfp_pwr_rx": -1840, "sfp_pwr_tx": 210}, true
	case "connection/lte/backup":
		return M{"state": "not_detected", "enabled": false}, true

	case "call/log/":
		return b.callLog(now), true
	case "call/account/":
		return M{"phone_number": "0199000100"}, true
	case "call/voicemail/":
		return L{M{"id": "1", "country_code": "33", "phone_number": "0639980142", "date": now - 3*3600, "duration": 24, "read": false},
			M{"id": "2", "country_code": "33", "phone_number": "0199000231", "date": now - 2*86400, "duration": 41, "read": true}}, true
	case "contact/":
		return b.contacts(), true

	case "fw/redir/":
		return b.redirs(now), true
	case "fw/dmz/":
		return M{"enabled": false, "ip": ""}, true
	case "fw/incoming/":
		return L{
			M{"id": "ftp", "type": "ftp", "enabled": false, "active": false, "in_port": 21, "min_port": 21, "max_port": 21, "readonly": false},
			M{"id": "ssh", "type": "ssh", "enabled": false, "active": false, "in_port": 22, "min_port": 22, "max_port": 22, "readonly": false},
			M{"id": "http", "type": "http", "enabled": false, "active": false, "in_port": 80, "min_port": 80, "max_port": 80, "readonly": false},
			M{"id": "https", "type": "https", "enabled": false, "active": false, "in_port": 443, "min_port": 443, "max_port": 443, "readonly": false},
		}, true
	case "upnpigd/config/":
		return M{"enabled": true, "version": 2}, true
	case "upnpigd/redir/":
		return L{M{"id": "udp/45012", "enabled": true, "proto": "udp", "ext_port": 45012, "int_ip": "192.168.1.70", "int_port": 45012,
			"desc": "Jeu en ligne", "remaining": 2400, "ext_src_ip": "0.0.0.0", "host": devices[9].host(now)}}, true

	case "switch/status/":
		return L{
			M{"id": 1, "name": "Port 1", "link": "up", "speed": "1000", "duplex": "full", "mode": "1000BaseT-FD", "rrd_id": "1",
				"mac_list": L{M{"mac": devices[4].mac, "hostname": "pc-de-marc"}}},
			M{"id": 2, "name": "Port 2", "link": "up", "speed": "2500", "duplex": "full", "mode": "2500BaseT-FD", "rrd_id": "2",
				"mac_list": L{M{"mac": devices[2].mac, "hostname": "nas-maison"}}},
			M{"id": 3, "name": "Port 3", "link": "up", "speed": "1000", "duplex": "full", "mode": "1000BaseT-FD", "rrd_id": "3",
				"mac_list": L{M{"mac": devices[1].mac, "hostname": "tv-du-salon"}}},
			M{"id": 4, "name": "Port 4", "link": "down", "speed": "0", "duplex": "half", "mode": "auto", "rrd_id": "4", "mac_list": L{}},
		}, true

	case "freeplug/":
		return L{M{"id": "02:00:00:20:00:00", "members": L{
			M{"id": "02:00:00:20:00:01", "local": true, "net_role": "cco", "model": "HD", "eth_port_status": "up", "eth_full_duplex": true, "has_network": true, "eth_speed": 100, "inactive": 0, "rx_rate": 220, "tx_rate": 190},
			M{"id": "02:00:00:20:00:02", "local": false, "net_role": "sta", "model": "HD", "eth_port_status": "up", "eth_full_duplex": true, "has_network": true, "eth_speed": 100, "inactive": 0, "rx_rate": 180, "tx_rate": 160},
		}}}, true

	case "notif/targets":
		return L{M{"id": "demo-1", "name": "Application mobile de démonstration", "type": "ios", "last_use": now - 7200, "api_url": "", "message_type": "", "subscriptions": L{"call", "download_done"}}}, true
	case "standby/status":
		return M{"use_planning": true, "planning_mode": "suspend", "available_planning_modes": L{"suspend", "wake"}, "next_change": now + 5*3600}, true
	case "standby/config/":
		return M{"use_planning": true, "planning_mode": "suspend", "resolution": 48, "mapping": b.weekPlan(func(day, slot int) bool {
			h := slot / 2
			return h >= 1 && h < 6 // standby at night
		})}, true
	case "lcd/config/":
		return M{"brightness": 70, "orientation": 0, "orientation_forced": false, "hide_wifi_key": true, "hide_status_led": false}, true
	case "player/":
		return L{M{"id": 1, "mac": devices[0].mac, "stb_type": "stb_v8", "device_name": "Freebox Player", "device_model": "fbxtv-pop", "reachable": true,
			"api_available": true, "api_version": "6.0", "last_time_reachable": now, "lan_gids": L{"pub"}}}, true

	case "netshare/samba/":
		return M{"file_share_enabled": true, "print_share_enabled": false, "logon_enabled": false, "logon_user": "freebox", "workgroup": "WORKGROUP", "smbv2_enabled": true}, true
	case "netshare/afp/":
		return M{"enabled": false, "guest_allow": false, "login_name": "freebox", "server_type": "macmini"}, true
	case "ftp/config/":
		return M{"enabled": false, "allow_anonymous": false, "allow_anonymous_write": false, "username": "freebox", "allow_remote_access": false,
			"weak_password": false, "port_ctrl": 21, "port_data": 20, "remote_domain": "demo.example.net"}, true
	case "tftp/config/":
		return M{"enabled": false, "root": "L0Zyb2JveC9QYXJ0YWdlcw=="}, true // base64 "/Freebox/Partages"
	case "upnpav/config/":
		return M{"enabled": true}, true
	case "airmedia/config/":
		return M{"enabled": true}, true
	case "airmedia/receivers/":
		return L{M{"name": "Freebox Player", "password_protected": false, "capabilities": M{"photo": true, "audio": true, "video": true, "screen": false}}}, true

	case "storage/disk/":
		return b.disks(), true
	case "storage/config/":
		return M{"external_pm_enabled": true, "external_pm_idle_before_spindown": 600}, true
	case "storage/raid/":
		return L{}, true

	case "vpn/":
		return L{
			M{"name": "openvpn_routed", "type": "openvpn", "state": "stopped", "connection_count": 0, "auth_connection_count": 0},
			M{"name": "openvpn_bridge", "type": "openvpn", "state": "stopped", "connection_count": 0, "auth_connection_count": 0},
			M{"name": "pptp", "type": "pptp", "state": "stopped", "connection_count": 0, "auth_connection_count": 0},
			M{"name": "wireguard", "type": "wireguard", "state": "started", "connection_count": 1, "auth_connection_count": 1},
		}, true
	case "vpn/user/":
		return L{
			M{"login": "marc", "type": "wireguard", "ip_reservation": "192.168.77.65", "password_set": false, "conf_wireguard": M{"keepalive": 25, "psk": true}},
			M{"login": "sophie", "type": "wireguard", "ip_reservation": "192.168.77.66", "password_set": false, "conf_wireguard": M{"keepalive": 25, "psk": true}},
			M{"login": "lucas", "type": "openvpn", "ip_reservation": "192.168.77.67", "password_set": true},
		}, true
	case "vpn/connection/":
		return L{M{"id": "wireguard-marc", "vpn": "wireguard", "user": "marc", "authenticated": true, "auth_time": now - 5400, "src_ip": "198.51.100.23",
			"src_port": 51820, "local_ip": "192.168.77.65", "rx_bytes": 48_200_000, "tx_bytes": 310_400_000}}, true
	case "vpn/ip_pool/":
		return M{"ip_start": "192.168.77.65", "ip_end": "192.168.77.94", "reservations": L{
			M{"login": "marc", "ip": "192.168.77.65"}, M{"login": "sophie", "ip": "192.168.77.66"}, M{"login": "lucas", "ip": "192.168.77.67"}}}, true
	case "vpn/openvpn_routed/config/", "vpn/openvpn_bridge/config/":
		return M{"id": strings.Split(path, "/")[1], "type": "openvpn", "enabled": false, "enable_ipv4": true, "enable_ipv6": false, "port": 1194, "min_port": 1024, "max_port": 65535,
			"ip_start": "192.168.77.65", "ip_end": "192.168.77.94", "conf_openvpn": M{"cipher": "aes256", "use_tcp": false, "disable_fragment": false}}, true
	case "vpn/pptp/config/":
		return M{"id": "pptp", "type": "pptp", "enabled": false, "enable_ipv4": true, "enable_ipv6": false, "port": 1723, "min_port": 1024, "max_port": 65535,
			"ip_start": "192.168.77.65", "ip_end": "192.168.77.94", "conf_pptp": M{"mppe": "require", "allowed_auth": M{"mschapv2": true, "chap": false, "pap": false}}}, true
	case "vpn/wireguard/config/":
		return M{"id": "wireguard", "type": "wireguard", "enabled": true, "enable_ipv4": true, "enable_ipv6": false, "port": 51820, "min_port": 1024, "max_port": 65535,
			"ip_start": "192.168.77.65", "ip_end": "192.168.77.94", "conf_wireguard": M{"mtu": 1360}}, true
	case "vpn_client/config/":
		return L{M{"id": "demo-client", "description": "Sortie en Europe (démo)", "type": "wireguard", "active": true,
			"conf_wireguard": M{"remote_addr": "vpn.example.net", "remote_port": 51820}}}, true
	case "vpn_client/status":
		return M{"enabled": true, "active_vpn": "demo-client", "active_vpn_description": "Sortie en Europe (démo)", "type": "wireguard", "state": "up",
			"last_error": "", "last_up": now - 86400, "last_try": now - 86400, "next_try": 0,
			"stats": M{"rate_up": 12_000, "rate_down": 85_000, "bytes_up": 1_400_000_000, "bytes_down": int64(9_800_000_000)},
			"ipv4":  M{"ip_mask": M{"ip": "198.51.100.77", "mask": "255.255.255.255"}, "gateway": "198.51.100.1", "dns": L{"198.51.100.2"}}}, true
	case "vpn_client/log":
		return "démarrage du client WireGuard\nlien établi avec vpn.example.net\ntunnel actif", true

	case "wifi/config/":
		return M{"enabled": true, "power_saving": false, "mac_filter_state": "disabled"}, true
	case "wifi/state/":
		return M{"state": "enabled", "power_saving_capability": "supported", "expected_phys": L{
			M{"band": "2d4g", "phy_id": 0, "detected": true}, M{"band": "5g", "phy_id": 1, "detected": true}, M{"band": "6g", "phy_id": 2, "detected": true}}}, true
	case "wifi/steering/config/":
		return M{"steering_level": 1}, true
	case "wifi/temp_disable":
		return M{"remaining": 0}, true
	case "wifi/diag":
		return M{"aps": L{}, "bsss": L{}}, true
	case "wifi/ap/":
		return L{b.ap(0, "2d4g", 20, 6, 0), b.ap(1, "5g", 80, 36, 0), b.ap(2, "6g", 160, 37, 0)}, true
	case "wifi/bss/":
		return b.bss(), true
	case "wifi/mac_filter/":
		return L{M{"id": "02:00:00:10:00:0a", "mac": devices[9].mac, "type": "blacklist", "comment": "Console de Lucas", "hostname": "console-de-lucas"}}, true
	case "wifi/wps/config/":
		return M{"enabled": true}, true
	case "wifi/wps/sessions/":
		return L{M{"id": 3, "ssid": "Maison", "active": false, "result": "success", "start_date": now - 6*86400, "end_date": now - 6*86400 + 38, "mac": devices[12].mac}}, true
	case "wifi/planning/":
		return M{"use_planning": true, "resolution": 48, "mapping": b.weekPlanStr(func(day, slot int) string {
			if h := slot / 2; h >= 1 && h < 6 {
				return "off"
			}
			return "on"
		})}, true
	case "wifi/custom_keys/config/":
		return M{"ssid": "Invités", "ssid_read_only": false, "hide_ssid": false, "encryption": "wpa2_psk_ccmp"}, true
	case "wifi/custom_key/":
		return L{
			M{"id": 1, "remaining": 2*86400 + 3600, "params": M{"description": "Visite de Claire", "key": "demo-invite-0001", "max_use_count": 3, "duration": 259200, "access_type": "net_only"},
				"users": L{M{"hostname": "telephone-de-claire"}}},
			M{"id": 2, "remaining": 0, "params": M{"description": "Plombier", "key": "demo-invite-0002", "max_use_count": 1, "duration": 86400, "access_type": "net_only"}, "users": L{}},
		}, true

	case "downloads/":
		return b.tasks(now), true
	case "downloads/stats":
		return M{"nb_tasks": 4, "nb_tasks_downloading": 1, "nb_tasks_seeding": 1, "nb_tasks_queued": 1, "nb_tasks_stopped": 0, "nb_tasks_done": 1, "nb_tasks_error": 0,
			"rx_rate": 4_200_000, "tx_rate": 380_000, "throttling_mode": "normal", "throttling_is_scheduled": false, "nb_peer": 37, "nb_rss": 1, "nb_rss_items_unread": 2, "conn_ready": true,
			"dht_stats": M{"enabled": true, "node_count": 412}, "nzb_config_status": M{"status": "ok", "error": ""}}, true
	case "downloads/config/":
		return b.dlConfig(), true
	case "downloads/feeds/":
		return L{M{"id": 1, "title": "Distributions Linux (démo)", "url": "https://feeds.example.net/linux.xml", "status": "ok", "nb_read": 12, "nb_unread": 2, "auto_download": false, "fetch_ts": now - 1800, "error": ""}}, true

	case "share_link/":
		return L{M{"token": "demo-token-0001", "path": "L0ZyZWVib3gvUGFydGFnZXM=", "name": "Partages", "expire": now + 5*86400, "fullurl": "https://demo.example.net/share/demo-token-0001"}}, true
	case "fs/tasks/":
		return L{}, true

	case "profile/":
		return L{M{"id": 1, "name": "Lucas", "url": ""}, M{"id": 2, "name": "Camille", "url": ""}}, true
	case "parental/config/":
		return M{"default_filter_mode": "allowed"}, true

	case "tv/bouquets/":
		return L{M{"id": 1, "name": "Bouquet de démonstration", "channel_count": len(tvChannels), "state": "enabled"}}, true
	case "tv/channels/":
		m := M{}
		for _, c := range tvChannels {
			m[c.uuid] = M{"uuid": c.uuid, "name": c.name, "short_name": c.name, "available": c.avail, "has_abo": true, "has_service": true, "logo_url": ""}
		}
		return m, true

	case "pvr/config/":
		return M{"margin_before": 120, "margin_after": 300, "auto_ack": true}, true
	case "pvr/quota/":
		return M{"quota_exceeded": false, "needed_tresh": 0, "cur_tresh": 0}, true
	case "pvr/media/":
		return L{M{"media": "Disque dur", "free_bytes": int64(640_000_000_000), "total_bytes": int64(1_000_000_000_000)}}, true
	case "pvr/programmed/":
		return L{b.record(101, "Le journal du soir", "Édition de 20 h", 1, now+4*3600, now+4*3600+2400, "waiting_start_time", 0),
			b.record(102, "Cinéma de minuit", "Comédie fictive", 3, now+30*3600, now+30*3600+6000, "waiting_start_time", 0)}, true
	case "pvr/finished/":
		return L{b.record(90, "Documentaire nature", "Les forêts du nord", 2, now-3*86400, now-3*86400+3000, "finished", 2_100_000_000),
			b.record(91, "Magazine cuisine", "Recettes d'hiver", 4, now-9*86400, now-9*86400+1800, "finished", 1_300_000_000)}, true
	}
	return nil, false
}

// weekPlan builds the 7 x 48 half-hour boolean mapping of a planning.
func (b *Box) weekPlan(on func(day, slot int) bool) L {
	out := make(L, 0, 336)
	for d := 0; d < 7; d++ {
		for s := 0; s < 48; s++ {
			out = append(out, on(d, s))
		}
	}
	return out
}

func (b *Box) weekPlanStr(f func(day, slot int) string) L {
	out := make(L, 0, 336)
	for d := 0; d < 7; d++ {
		for s := 0; s < 48; s++ {
			out = append(out, f(d, s))
		}
	}
	return out
}

func (b *Box) redirs(now int64) L {
	nas, cam, tv := devices[2], devices[10], devices[4]
	mk := func(id int, comment, proto string, start, end int, d device, lanPort int, enabled bool) M {
		return M{"id": id, "enabled": enabled, "comment": comment, "valid": true, "ip_proto": proto, "wan_port_start": start, "wan_port_end": end,
			"lan_ip": d.ip, "lan_port": lanPort, "src_ip": "0.0.0.0", "hostname": toHostname(d.name), "host": d.host(now)}
	}
	return L{mk(1, "Serveur web du NAS", "tcp", 8080, 8080, nas, 8080, true), mk(2, "Caméra (démo)", "tcp", 8554, 8554, cam, 554, false), mk(3, "Bureau à distance", "tcp", 33890, 33890, tv, 3389, false)}
}

func (b *Box) ap(id int, band string, width, primary, secondary int) M {
	w := strconv.Itoa(width)
	cfg := M{"band": band, "channel_width": w, "primary_channel": primary, "secondary_channel": secondary, "dfs_enabled": band == "5g"}
	st := M{"state": "active", "channel_width": w, "primary_channel": primary, "secondary_channel": secondary, "dfs_cac_remaining_time": 0}
	return M{"id": id, "name": "WLAN " + strconv.Itoa(id), "config": cfg, "status": st, "capabilities": M{}}
}

func (b *Box) bss() L {
	mk := func(id, phy int, ssid, key string, guest bool, sta int) M {
		cfg := M{"enabled": true, "ssid": ssid, "encryption": "wpa2_psk_ccmp", "hide_ssid": false, "key": key, "wps_enabled": !guest, "eapol_version": 2}
		if phy == 2 {
			cfg["encryption"] = "wpa3_psk_ccmp"
		}
		return M{"id": "02:00:00:00:0" + strconv.Itoa(phy) + ":0" + strconv.Itoa(id), "phy_id": phy, "use_shared_params": !guest,
			"config": cfg, "bss_params": cfg, "shared_bss_params": cfg,
			"status": M{"state": "active", "sta_count": sta, "authorized_sta_count": sta, "band": "", "is_main_bss": !guest}}
	}
	return L{
		mk(1, 0, "Maison", "demo-maison-0000", false, 6), mk(2, 1, "Maison", "demo-maison-0000", false, 5), mk(3, 2, "Maison", "demo-maison-0000", false, 1),
		mk(4, 0, "Invités", "demo-invite-0000", true, 1),
	}
}
