package demo

import (
	"encoding/base64"
	"strconv"
	"strings"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// dynamic answers the paths that carry an identifier.
func (b *Box) dynamic(path string, now int64) (any, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case parts[0] == "connection" && len(parts) >= 3 && parts[1] == "ddns":
		return b.ddns(parts[2], len(parts) > 3 && parts[3] == "status", now), true
	case parts[0] == "network_control" && len(parts) >= 2:
		id, _ := strconv.Atoi(parts[1])
		if len(parts) >= 3 && parts[2] == "rules" {
			return b.rules(id), true
		}
		return b.networkControl(id, now), true
	case parts[0] == "player" && len(parts) >= 5 && parts[2] == "api":
		if parts[4] == "status" {
			return M{"power_state": "running", "player": M{"name": "Freebox Player"}}, true
		}
		return M{"volume": 35, "mute": false}, true
	case parts[0] == "tv" && len(parts) >= 3 && parts[1] == "bouquets":
		out := L{}
		for i, c := range tvChannels {
			out = append(out, M{"number": i + 1, "uuid": c.uuid, "available": c.avail})
		}
		return out, true
	case parts[0] == "tv" && len(parts) >= 3 && parts[1] == "epg":
		return epg(now), true
	case parts[0] == "switch" && len(parts) >= 3 && parts[1] == "port":
		id, _ := strconv.Atoi(parts[2])
		if len(parts) > 3 {
			return M{"rx_good_bytes": 52_000_000_000 * int64(id), "tx_bytes": 9_000_000_000 * int64(id), "rx_bytes_rate": 12_000 * int64(id), "tx_bytes_rate": 3_000 * int64(id),
				"rx_good_packets": 41_000_000 * int64(id), "tx_packets": 22_000_000 * int64(id), "rx_err_packets": 0, "rx_fcs_packets": 0, "tx_collisions": 0}, true
		}
		return M{"speed": "auto", "duplex": "auto"}, true
	case parts[0] == "wifi" && len(parts) >= 3 && parts[1] == "ap":
		id, _ := strconv.Atoi(parts[2])
		sub := ""
		if len(parts) > 3 {
			sub = parts[3]
		}
		return b.apSub(id, sub, now)
	case parts[0] == "vpn" && len(parts) >= 3 && parts[1] == "download_config":
		return "# Configuration fictive du mode démonstration\n", true
	case parts[0] == "fs" && len(parts) >= 3 && parts[1] == "ls":
		return b.ls(parts[2], now), true
	case parts[0] == "downloads" && len(parts) >= 2:
		return b.dlSub(parts[1:], now)
	case parts[0] == "profile" && len(parts) >= 2:
		id, _ := strconv.Atoi(parts[1])
		return M{"id": id, "name": map[int]string{1: "Lucas", 2: "Camille"}[id], "url": ""}, true
	}
	return nil, false
}

func (b *Box) ddns(provider string, status bool, now int64) any {
	if status {
		if provider == "ovh" {
			return M{"status": "up-to-date", "last_refresh": now - 1800, "next_refresh": now + 1800, "next_retry": 0, "last_error": 0}
		}
		return M{"status": "disabled", "last_refresh": 0, "next_refresh": 0, "next_retry": 0, "last_error": 0}
	}
	if provider == "ovh" {
		return M{"enabled": true, "hostname": "maison.example.net", "user": "demo-user"}
	}
	return M{"enabled": false, "hostname": "", "user": ""}
}

func (b *Box) networkControl(id int, now int64) any {
	macs := L{devices[8].mac, devices[9].mac}
	if id == 2 {
		macs = L{devices[5].mac}
	}
	return M{"current_mode": "allowed", "rule_mode": "allowed", "override": false, "override_mode": "allowed", "override_until": 0,
		"next_change": now + 4*3600, "macs": macs, "profile_id": id}
}

func (b *Box) rules(id int) any {
	if id == 2 {
		return L{}
	}
	return L{
		M{"id": 11, "name": "Soirée d'école", "mode": "denied", "start_time": 20*3600 + 30*60, "end_time": 24*3600 - 1, "weekdays": L{true, true, true, true, false, false, true}, "enabled": true},
		M{"id": 12, "name": "Nuit", "mode": "denied", "start_time": 0, "end_time": 7 * 3600, "weekdays": L{true, true, true, true, true, true, true}, "enabled": true},
	}
}

// ls is a small file tree, keyed by the base64 path the page sends.
func (b *Box) ls(enc string, now int64) any {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return M{"entries": L{}}
	}
	dir := func(name, path string) M {
		return M{"name": name, "path": b64(path), "type": "dir", "index": 0, "link": false, "modification": now - 86400*20, "hidden": false, "mimetype": "inode/directory", "size": 4096}
	}
	file := func(name, path, mime string, size int64, age int64) M {
		return M{"name": name, "path": b64(path), "type": "file", "index": 0, "link": false, "modification": now - age, "hidden": false, "mimetype": mime, "size": size}
	}
	switch string(raw) {
	case "/":
		return M{"entries": L{dir("Freebox", "/Freebox")}}
	case "/Freebox":
		return M{"entries": L{dir("Enregistrements", "/Freebox/Enregistrements"), dir("Musique", "/Freebox/Musique"), dir("Partages", "/Freebox/Partages"),
			dir("Photos", "/Freebox/Photos"), dir("Téléchargements", "/Freebox/Téléchargements"), dir("Vidéos", "/Freebox/Vidéos")}}
	case "/Freebox/Partages":
		return M{"entries": L{file("budget-2026.ods", "/Freebox/Partages/budget-2026.ods", "application/vnd.oasis.opendocument.spreadsheet", 48_200, 3*86400),
			file("recettes.pdf", "/Freebox/Partages/recettes.pdf", "application/pdf", 1_240_000, 40*86400)}}
	case "/Freebox/Téléchargements":
		return M{"entries": L{file("distribution-linux-demo.iso", "/Freebox/Téléchargements/distribution-linux-demo.iso", "application/x-iso9660-image", 2_900_000_000, 86400)}}
	case "/Freebox/Photos":
		return M{"entries": L{dir("Vacances 2025", "/Freebox/Photos/Vacances 2025"), file("anniversaire.jpg", "/Freebox/Photos/anniversaire.jpg", "image/jpeg", 3_400_000, 12*86400)}}
	case "/Freebox/Enregistrements":
		return M{"entries": L{file("Documentaire nature.ts", "/Freebox/Enregistrements/Documentaire nature.ts", "video/mp2t", 2_100_000_000, 3*86400)}}
	}
	return M{"entries": L{}}
}

func (b *Box) callLog(now int64) L {
	type c struct {
		typ, name, num string
		ago, dur       int64
		contact        int
		isNew          bool
	}
	cs := []c{
		{"missed", "Julien Bernard", "0639980118", 1800, 0, 3, true},
		{"accepted", "Camille Durand", "0199000254", 5 * 3600, 312, 2, false},
		{"outgoing", "Plomberie Lambert", "0199000377", 26 * 3600, 95, 5, false},
		{"missed", "", "0639980390", 30 * 3600, 0, 0, true},
		{"accepted", "Mamie Odette", "0199000412", 2 * 86400, 1260, 7, false},
		{"outgoing", "Dr Moreau", "0199000488", 3 * 86400, 140, 6, false},
		{"accepted", "Léa Petit", "0639980221", 4 * 86400, 604, 4, false},
		{"missed", "Claire Roux", "0639980567", 5 * 86400, 0, 8, false},
		{"outgoing", "Camille Durand", "0199000254", 6 * 86400, 48, 2, false},
		{"accepted", "Julien Bernard", "0639980118", 8 * 86400, 233, 3, false},
	}
	out := L{}
	for i, x := range cs {
		out = append(out, M{"id": 100 - i, "type": x.typ, "datetime": now - x.ago, "number": x.num, "name": x.name, "duration": x.dur, "contact_id": x.contact, "new": x.isNew})
	}
	return out
}

func (b *Box) contacts() L {
	mk := func(id int, first, last, company, num, typ, mail string) M {
		c := M{"id": id, "display_name": strings.TrimSpace(first + " " + last), "first_name": first, "last_name": last, "company": company, "notes": "",
			"numbers": L{M{"id": id*10 + 1, "type": typ, "number": num, "is_default": true, "is_own": false}},
			"emails":  L{}, "addresses": L{}, "urls": L{}}
		if mail != "" {
			c["emails"] = L{M{"id": id*10 + 2, "type": "home", "email": mail}}
		}
		return c
	}
	return L{
		mk(2, "Camille", "Durand", "", "0199000254", "fixed", "camille@example.com"),
		mk(3, "Julien", "Bernard", "", "0639980118", "mobile", "julien@example.com"),
		mk(4, "Léa", "Petit", "", "0639980221", "mobile", ""),
		mk(5, "", "", "Plomberie Lambert", "0199000377", "work", ""),
		mk(6, "Dr", "Moreau", "", "0199000488", "work", ""),
		mk(7, "Mamie", "Odette", "", "0199000412", "fixed", ""),
		mk(8, "Claire", "Roux", "", "0639980567", "mobile", "claire@example.com"),
	}
}

func (b *Box) disks() L {
	const total = 1_000_204_886_016
	return L{M{"id": 1, "type": "internal", "state": "enabled", "model": "Disque fictif 1 To", "serial": "DEMO-DISK-0001", "firmware": "DM01",
		"table_type": "gpt", "total_bytes": total, "temp": 38, "spinning": true, "read_error_requests": 0, "write_error_requests": 0,
		"operation_pct": M{"percent": 0},
		"partitions": L{M{"id": 1, "disk_id": 1, "state": "mounted", "fstype": "ext4", "label": "Freebox", "total_bytes": total - 2_000_000_000,
			"used_bytes": 412_000_000_000, "free_bytes": total - 2_000_000_000 - 412_000_000_000, "fsck_result": "no_run_yet", "operation_pct": M{"percent": 0}}}}}
}

func (b *Box) tasks(now int64) L {
	dir := b64("/Freebox/Téléchargements")
	mk := func(id int, typ, name, status string, size int64, rxPct, txPct int, rxRate, txRate int64, eta int64) M {
		return M{"id": id, "type": typ, "name": name, "status": status, "size": size, "queue_pos": id, "io_priority": "normal",
			"rx_bytes": size * int64(rxPct) / 10000, "tx_bytes": size * int64(txPct) / 10000, "rx_rate": rxRate, "tx_rate": txRate, "rx_pct": rxPct, "tx_pct": txPct,
			"error": "none", "created_ts": now - int64(id)*7200, "eta": eta, "download_dir": dir, "stop_ratio": 150, "info_hash": strings.Repeat("0", 38) + strconv.Itoa(10+id)}
	}
	return L{
		mk(1, "bt", "distribution-linux-demo.iso", "downloading", 2_900_000_000, 6420, 0, 4_200_000, 310_000, 1800),
		mk(2, "bt", "documents-libres-2026", "seeding", 780_000_000, 10000, 4300, 0, 70_000, 0),
		mk(3, "http", "manuel-utilisateur.pdf", "queued", 18_000_000, 0, 0, 0, 0, 0),
		mk(4, "http", "carte-regionale.zip", "done", 240_000_000, 10000, 0, 0, 0, 0),
	}
}

func (b *Box) dlConfig() M {
	sched := make(L, 168)
	for i := range sched {
		sched[i] = "normal"
		if h := i % 24; h >= 19 && h < 23 && i/24 < 5 {
			sched[i] = "slow"
		}
	}
	return M{
		"max_downloading_tasks": 5, "download_dir": b64("/Freebox/Téléchargements"), "watch_dir": b64("/Freebox/Téléchargements/À télécharger"), "use_watch_dir": false,
		"dns1": "", "dns2": "", "blocklist": M{"sources": M{}},
		"throttling": M{"mode": "normal", "normal": M{"rx_rate": 0, "tx_rate": 0}, "slow": M{"rx_rate": 512_000, "tx_rate": 42_000}, "schedule": sched},
		"bt":         M{"max_peers": 50, "stop_ratio": 150, "crypto_support": "allowed", "enable_dht": true, "enable_pex": true, "announce_timeout": 30, "main_port": 51413, "dht_port": 51413},
		"news":       M{"server": "news.example.org", "port": 563, "ssl": true, "user": "demo", "nthreads": 4, "auto_repair": true, "lazy_par2": true, "auto_extract": true, "erase_tmp": true, "file_naming": "default"},
		"feed":       M{"fetch_interval": 60, "max_items": 100},
	}
}

func (b *Box) dlSub(parts []string, now int64) (any, bool) {
	if parts[0] == "feeds" {
		if len(parts) == 2 {
			return M{"id": 1, "title": "Distributions Linux (démo)", "url": "https://feeds.example.net/linux.xml", "status": "ok", "nb_read": 12, "nb_unread": 2, "auto_download": false, "fetch_ts": now - 1800, "error": ""}, true
		}
		return L{
			M{"id": 1, "title": "distribution-linux-demo 26.04", "author": "Équipe fictive", "link": "https://feeds.example.net/linux/26.04", "is_read": false, "is_downloaded": false, "pub_ts": now - 86400},
			M{"id": 2, "title": "distribution-linux-demo 25.10", "author": "Équipe fictive", "link": "https://feeds.example.net/linux/25.10", "is_read": true, "is_downloaded": true, "pub_ts": now - 180*86400},
		}, true
	}
	id, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, false
	}
	tasks := b.tasks(now)
	if id < 1 || id > len(tasks) {
		return nil, false
	}
	if len(parts) == 1 {
		return tasks[id-1], true
	}
	switch parts[1] {
	case "files":
		return L{M{"id": "1", "name": "contenu/image.iso", "size": 2_900_000_000, "rx": 1_861_800_000, "status": "downloading", "priority": "normal", "error": "none"}}, true
	case "log":
		return "tâche démarrée\nconnexion aux pairs\ntéléchargement en cours", true
	case "trackers":
		return L{M{"announce": "https://tracker.example.net/announce", "is_backup": false, "status": "working", "reannounce_in": 900, "nseeders": 120, "nleechers": 14, "is_enabled": true}}, true
	case "peers":
		return L{M{"host": "198.51.100.40", "port": 51413, "state": "connected", "origin": "tracker", "protocol": "bt", "client": "ClientFictif 1.0", "country_code": "FR",
			"progress": 100, "rx": 120_000_000, "tx": 4_000_000, "rx_rate": 800_000, "tx_rate": 20_000}}, true
	case "pieces":
		return strings.Repeat("X", 64) + strings.Repeat(".", 36), true
	case "blacklist":
		return L{}, true
	}
	return nil, false
}

type tvChan struct {
	uuid, name string
	avail      bool
}

var tvChannels = []tvChan{
	{"uuid-demo-1", "Première Info", true}, {"uuid-demo-2", "Chaîne Découverte", true}, {"uuid-demo-3", "Cinéma Plus", true},
	{"uuid-demo-4", "Sport Direct", true}, {"uuid-demo-5", "Culture TV", true}, {"uuid-demo-6", "Mini Junior", true},
	{"uuid-demo-7", "Cuisine et Saveurs", true}, {"uuid-demo-8", "Nature 24", true}, {"uuid-demo-9", "Musique Live", true},
	{"uuid-demo-10", "Séries Max", true}, {"uuid-demo-11", "Actu 24", true}, {"uuid-demo-12", "Voyage", false},
}

func epg(now int64) M {
	titles := [][3]string{
		{"Le journal", "Édition du jour", "Information"}, {"Documentaire nature", "Les forêts du nord", "Documentaire"},
		{"Cinéma de minuit", "Comédie fictive", "Film"}, {"Match en direct", "Championnat fictif", "Sport"},
	}
	out := M{}
	start := now - now%3600
	for ci, c := range tvChannels {
		progs := M{}
		for k := 0; k < 4; k++ {
			t := titles[(ci+k)%len(titles)]
			id := "p" + strconv.Itoa(ci) + "_" + strconv.Itoa(k)
			p := M{"title": t[0], "sub_title": t[1], "category_name": t[2], "date": start + int64(k)*3600, "duration": 3600}
			if k < 3 {
				p["next"] = "p" + strconv.Itoa(ci) + "_" + strconv.Itoa(k+1)
			}
			progs[id] = p
		}
		out[c.uuid] = progs
	}
	return out
}

func (b *Box) record(id int, name, sub string, ch int, start, end int64, state string, size int64) M {
	return M{"id": id, "name": name, "subname": sub, "channel_name": tvChannels[ch-1].name, "start": start, "end": end, "state": state, "error": "none",
		"conflict": false, "altered": false, "enabled": true, "has_record_gen": state == "finished", "media": "Disque dur", "byte_size": size,
		"filename": strings.ReplaceAll(strings.ToLower(name), " ", "-") + ".ts"}
}
