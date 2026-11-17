package server

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Downloads: the box's download manager (HTTP, FTP, magnet, BitTorrent,
// newsgroups): queue, per-task details, RSS feeds and settings.
//
// Known limits of the API client, which refuses an encoded "/" inside a
// path segment:
//   - "downloads/add" goes form-urlencoded (the op's Form flag), as the box
//     documents; .torrent file upload is not offered;
//   - removing or updating a tracker (the announce URL goes in the path) is
//     not possible.

func init() {
	register(module{Slug: "downloads", Title: "Téléchargements", Group: "Contenus", Order: 0, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /downloads", s.downloads)
		s.page(mux, "GET /downloads/{id}", s.downloadDetail)
		s.page(mux, "GET /downloads/feeds/{fid}", s.feedItems)
		s.page(mux, "POST /downloads/add", s.downloadAdd)
		s.page(mux, "POST /downloads/config", s.downloadsConfig)
		s.page(mux, "POST /downloads/throttling", s.downloadsThrottling)
		s.page(mux, "POST /downloads/schedule", s.downloadsSchedule)
		s.page(mux, "POST /downloads/{id}/status", s.downloadStatus)
		s.page(mux, "POST /downloads/{id}/priority", s.downloadPriority)
		s.page(mux, "POST /downloads/{id}/delete", s.downloadDelete)
		s.page(mux, "POST /downloads/{id}/erase", s.downloadErase)
		s.page(mux, "POST /downloads/{id}/file", s.downloadFile)
		s.page(mux, "POST /downloads/{id}/tracker", s.downloadTracker)
		s.page(mux, "POST /downloads/{id}/block", s.downloadBlock)
		s.page(mux, "POST /downloads/{id}/unblock", s.downloadUnblock)
		s.page(mux, "POST /downloads/{id}/clear-blacklist", s.downloadClearBlacklist)
		s.page(mux, "POST /downloads/feeds/add", s.feedAdd)
		s.page(mux, "POST /downloads/feeds/fetch", s.feedFetchAll)
		s.page(mux, "POST /downloads/feeds/{fid}/fetch", s.feedFetch)
		s.page(mux, "POST /downloads/feeds/{fid}/auto", s.feedAuto)
		s.page(mux, "POST /downloads/feeds/{fid}/delete", s.feedDelete)
		s.page(mux, "POST /downloads/feeds/{fid}/read", s.feedReadAll)
		s.page(mux, "POST /downloads/feeds/{fid}/items/{iid}/download", s.feedItemDownload)
		s.page(mux, "POST /downloads/feeds/{fid}/items/{iid}/read", s.feedItemRead)
	}})
	id := ops.Param{Name: "id", Type: "integer", Description: "Download task id", Required: true}
	fid := ops.Param{Name: "feed_id", Type: "integer", Description: "RSS feed id", Required: true}
	item := ops.Param{Name: "item_id", Type: "integer", Description: "Feed item id", Required: true}
	ops.Register(
		ops.Op{Name: "downloads_list", Description: "List the download tasks: id, type (bt, nzb, http, ftp), name, status, size, rx_pct/tx_pct (percent x100), rx_rate/tx_rate (bytes/s), eta, io_priority, error, download_dir (base64).",
			Path: "downloads/"},
		ops.Op{Name: "downloads_get", Description: "Get one download task.", Path: "downloads/{id}", Params: []ops.Param{id}},
		ops.Op{Name: "downloads_update", Description: "Pause, resume or retry a task (status: stopped, downloading, retry, seeding) or change its io_priority (low, normal, high).",
			Method: http.MethodPut, Path: "downloads/{id}", Params: []ops.Param{id}, Body: true,
			BodyHelp: `{"status": "stopped", "io_priority": "high"}`},
		ops.Op{Name: "downloads_delete", Description: "Remove a task from the list; the downloaded files are kept. A running task is stopped.",
			Method: http.MethodDelete, Path: "downloads/{id}", Params: []ops.Param{id}},
		ops.Op{Name: "downloads_erase", Description: "Remove a task AND erase its downloaded files.",
			Method: http.MethodDelete, Path: "downloads/{id}/erase", Params: []ops.Param{id}, Dangerous: true},
		ops.Op{Name: "downloads_log", Description: "Get the log of a task (text).", Path: "downloads/{id}/log", Params: []ops.Param{id}},
		ops.Op{Name: "downloads_add", Description: "Add a download by URL (http://, ftp://, magnet:). The box fetches the address itself. Returns the new task id (or a list of ids with download_url_list).",
			Method: http.MethodPost, Path: "downloads/add", Body: true, Form: true, Dangerous: true,
			BodyHelp: `{"download_url": "magnet:?xt=urn:btih:...", "download_dir": "<base64 of /Disque dur/Téléchargements/>", "recursive": false, "username": "", "password": ""}; several URLs: "download_url_list": "url1\nurl2"`},
		ops.Op{Name: "downloads_stats", Description: "Downloader summary: task counts per status, rx_rate/tx_rate (bytes/s), throttling_mode, nb_peer, nb_rss, nb_rss_items_unread, conn_ready, dht_stats, nzb_config_status.",
			Path: "downloads/stats"},
		ops.Op{Name: "downloads_files_list", Description: "List the files of a task: id, name, size, rx (bytes received), status, priority (no_dl, low, normal, high), error.",
			Path: "downloads/{id}/files", Params: []ops.Param{id}},
		ops.Op{Name: "downloads_file_update", Description: "Change the priority of one file of a task (no_dl skips it).",
			Method: http.MethodPut, Path: "downloads/{id}/files/{file_id}",
			Params: []ops.Param{id, {Name: "file_id", Description: `File id, e.g. "5-1"`, Required: true}},
			Body:   true, BodyHelp: `{"priority": "high"}`},
		ops.Op{Name: "downloads_trackers_list", Description: "List the trackers of a BitTorrent task: announce, status, seeders, leechers, reannounce_in, is_enabled, is_backup.",
			Path: "downloads/{id}/trackers", Params: []ops.Param{id}},
		ops.Op{Name: "downloads_tracker_add", Description: "Add a tracker to a BitTorrent task. (Removing or editing a tracker is not available: its announce URL cannot go in the path through this client.)",
			Method: http.MethodPost, Path: "downloads/{id}/trackers", Params: []ops.Param{id}, Body: true, Dangerous: true,
			BodyHelp: `{"announce": "udp://tracker.example.org:80"}`},
		ops.Op{Name: "downloads_peers_list", Description: "List the peers of a BitTorrent task: host, port, state, origin, protocol, client, country_code, progress, rx/tx and rates.",
			Path: "downloads/{id}/peers", Params: []ops.Param{id}},
		ops.Op{Name: "downloads_pieces_get", Description: "Get the piece map of a BitTorrent task as a string, one character per piece (X done, . wanted, - skipped).",
			Path: "downloads/{id}/pieces", Params: []ops.Param{id}},
		ops.Op{Name: "downloads_blacklist_list", Description: "List the blacklisted peers of a BitTorrent task: host, reason, expire (seconds), global.",
			Path: "downloads/{id}/blacklist", Params: []ops.Param{id}},
		ops.Op{Name: "downloads_blacklist_empty", Description: "Empty the blacklist of a task (also removes the global entries).",
			Method: http.MethodDelete, Path: "downloads/{id}/blacklist/empty", Params: []ops.Param{id}},
		ops.Op{Name: "downloads_blacklist_delete", Description: "Remove one host from the downloader blacklist.",
			Method: http.MethodDelete, Path: "downloads/blacklist/{host}", Params: []ops.Param{{Name: "host", Description: "Peer IP address", Required: true}}},
		ops.Op{Name: "downloads_blacklist_add", Description: "Blacklist a peer host for expire seconds.",
			Method: http.MethodPost, Path: "downloads/blacklist", Body: true, BodyHelp: `{"host": "192.0.2.7", "expire": 3600}`},
		ops.Op{Name: "downloads_feeds_list", Description: "List the RSS feeds: id, title, url, status, nb_read, nb_unread, auto_download, fetch_ts, error.",
			Path: "downloads/feeds/"},
		ops.Op{Name: "downloads_feed_get", Description: "Get one RSS feed.", Path: "downloads/feeds/{feed_id}", Params: []ops.Param{fid}},
		ops.Op{Name: "downloads_feed_add", Description: "Subscribe to an RSS feed.", Method: http.MethodPost, Path: "downloads/feeds/",
			Body: true, Dangerous: true, BodyHelp: `{"url": "https://example.org/rss.xml"}`},
		ops.Op{Name: "downloads_feed_update", Description: "Turn automatic download of new items on or off for a feed.",
			Method: http.MethodPut, Path: "downloads/feeds/{feed_id}", Params: []ops.Param{fid}, Body: true, BodyHelp: `{"auto_download": true}`},
		ops.Op{Name: "downloads_feed_delete", Description: "Delete an RSS feed and its items (the download tasks are kept).",
			Method: http.MethodDelete, Path: "downloads/feeds/{feed_id}", Params: []ops.Param{fid}},
		ops.Op{Name: "downloads_feed_fetch", Description: "Refresh one RSS feed now (fails with feed_is_recent if the feed's TTL has not expired).",
			Method: http.MethodPost, Path: "downloads/feeds/{feed_id}/fetch", Params: []ops.Param{fid}},
		ops.Op{Name: "downloads_feeds_fetch_all", Description: "Refresh every RSS feed now.", Method: http.MethodPost, Path: "downloads/feeds/fetch"},
		ops.Op{Name: "downloads_feed_items_list", Description: "List the items of an RSS feed: id, title, link, author, is_read, is_downloaded, pub_ts.",
			Path: "downloads/feeds/{feed_id}/items/", Params: []ops.Param{fid}},
		ops.Op{Name: "downloads_feed_item_update", Description: "Mark a feed item as read or unread.",
			Method: http.MethodPut, Path: "downloads/feeds/{feed_id}/items/{item_id}",
			Params: []ops.Param{fid, item}, Body: true, BodyHelp: `{"is_read": true}`},
		ops.Op{Name: "downloads_feed_item_download", Description: "Queue a feed item as a new download task.",
			Method: http.MethodPost, Path: "downloads/feeds/{feed_id}/items/{item_id}/download", Params: []ops.Param{fid, item}},
		ops.Op{Name: "downloads_feed_items_mark_read", Description: "Mark every item of a feed as read.",
			Method: http.MethodPost, Path: "downloads/feeds/{feed_id}/items/mark_all_as_read", Params: []ops.Param{fid}},
		ops.Op{Name: "downloads_config_get", Description: "Get the downloader settings: download_dir/watch_dir (base64), max_downloading_tasks, throttling (normal/slow rates in bytes/s, 168-slot weekly schedule, mode), bt, news (the NNTP password is never returned), feed, blocklist, dns1/dns2.",
			Path: "downloads/config/"},
		ops.Op{Name: "downloads_config_update", Description: "Update the downloader settings; send only the fields to change. A news.password is write-only.",
			Method: http.MethodPut, Path: "downloads/config/", Body: true,
			BodyHelp: `{"max_downloading_tasks": 5, "throttling": {"normal": {"rx_rate": 0, "tx_rate": 0}, "slow": {"rx_rate": 512000, "tx_rate": 42000}}, "bt": {"max_peers": 50, "stop_ratio": 150, "crypto_support": "allowed", "enable_dht": true}, "news": {"server": "news.example.org", "port": 119, "user": "", "password": ""}, "feed": {"fetch_interval": 60}}`},
		ops.Op{Name: "downloads_throttling_set", Description: "Force the bandwidth mode: normal, slow, hibernate, or schedule (follow the weekly schedule).",
			Method: http.MethodPut, Path: "downloads/throttling", Body: true, BodyHelp: `{"throttling": "slow"}`},
	)
}

// --- data ---

type dlTask struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Size        int64  `json:"size"`
	QueuePos    int    `json:"queue_pos"`
	IOPriority  string `json:"io_priority"`
	TxBytes     int64  `json:"tx_bytes"`
	RxBytes     int64  `json:"rx_bytes"`
	TxRate      int64  `json:"tx_rate"`
	RxRate      int64  `json:"rx_rate"`
	TxPct       int    `json:"tx_pct"`
	RxPct       int    `json:"rx_pct"`
	Error       string `json:"error"`
	CreatedTS   int64  `json:"created_ts"`
	ETA         int64  `json:"eta"`
	DownloadDir string `json:"download_dir"`
	StopRatio   int    `json:"stop_ratio"`
	InfoHash    string `json:"info_hash"`
}

var dlStatusFR = map[string]string{
	"stopped": "en pause", "queued": "en attente", "starting": "démarrage", "downloading": "en cours",
	"stopping": "arrêt…", "error": "erreur", "done": "terminé", "checking": "vérification",
	"repairing": "réparation", "extracting": "extraction", "seeding": "partage", "retry": "nouvel essai",
}

// StatusLabel is the status in French.
func (t dlTask) StatusLabel() string {
	if l, ok := dlStatusFR[t.Status]; ok {
		return l
	}
	return t.Status
}

// Pct is the progress in percent: the share for a seeding task, the download otherwise.
func (t dlTask) Pct() int {
	if t.Status == "seeding" {
		return min(t.TxPct/100, 100)
	}
	return min(t.RxPct/100, 100)
}

// Dir is the download folder, decoded.
func (t dlTask) Dir() string { return dlDecodePath(t.DownloadDir) }

func (t dlTask) Active() bool {
	switch t.Status {
	case "downloading", "queued", "starting", "seeding", "checking":
		return true
	}
	return false
}
func (t dlTask) CanResume() bool { return t.Status == "stopped" }
func (t dlTask) CanRetry() bool  { return t.Status == "error" }
func (t dlTask) CanSeed() bool   { return t.Status == "done" && t.Type == "bt" }
func (t dlTask) IsBT() bool      { return t.Type == "bt" }

// Ratio is the transmit/receive ratio of a BitTorrent task.
func (t dlTask) Ratio() string {
	if t.RxBytes == 0 {
		return "0,00"
	}
	return strings.Replace(strconv.FormatFloat(float64(t.TxBytes)/float64(t.RxBytes), 'f', 2, 64), ".", ",", 1)
}

func dlDecodePath(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return s
	}
	return string(b)
}

func encodePath(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

type dlFile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Rx       int64  `json:"rx"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	Error    string `json:"error"`
}

type dlTracker struct {
	Announce     string `json:"announce"`
	IsBackup     bool   `json:"is_backup"`
	Status       string `json:"status"`
	ReannounceIn int64  `json:"reannounce_in"`
	NSeeders     int    `json:"nseeders"`
	NLeechers    int    `json:"nleechers"`
	IsEnabled    bool   `json:"is_enabled"`
}

type dlPeer struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	State       string `json:"state"`
	Origin      string `json:"origin"`
	Protocol    string `json:"protocol"`
	Client      string `json:"client"`
	CountryCode string `json:"country_code"`
	Progress    int    `json:"progress"`
	Rx          int64  `json:"rx"`
	Tx          int64  `json:"tx"`
	RxRate      int64  `json:"rx_rate"`
	TxRate      int64  `json:"tx_rate"`
}

type dlBlock struct {
	Host   string `json:"host"`
	Reason string `json:"reason"`
	Expire int64  `json:"expire"`
	Global bool   `json:"global"`
}

type dlFeed struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	Status       string `json:"status"`
	NbRead       int    `json:"nb_read"`
	NbUnread     int    `json:"nb_unread"`
	AutoDownload bool   `json:"auto_download"`
	FetchTS      int64  `json:"fetch_ts"`
	Error        string `json:"error"`
}

// Total is the number of items of the feed.
func (f dlFeed) Total() int { return f.NbRead + f.NbUnread }

type dlFeedItem struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Author       string `json:"author"`
	Link         string `json:"link"`
	IsRead       bool   `json:"is_read"`
	IsDownloaded bool   `json:"is_downloaded"`
	PubTS        int64  `json:"pub_ts"`
}

type dlStats struct {
	NbTasks     int    `json:"nb_tasks"`
	Downloading int    `json:"nb_tasks_downloading"`
	Seeding     int    `json:"nb_tasks_seeding"`
	Queued      int    `json:"nb_tasks_queued"`
	Stopped     int    `json:"nb_tasks_stopped"`
	Done        int    `json:"nb_tasks_done"`
	Errors      int    `json:"nb_tasks_error"`
	RxRate      int64  `json:"rx_rate"`
	TxRate      int64  `json:"tx_rate"`
	Mode        string `json:"throttling_mode"`
	Scheduled   bool   `json:"throttling_is_scheduled"`
	NbPeer      int    `json:"nb_peer"`
	NbRSS       int    `json:"nb_rss"`
	RSSUnread   int    `json:"nb_rss_items_unread"`
	ConnReady   bool   `json:"conn_ready"`
	Dht         struct {
		Enabled bool `json:"enabled"`
		Nodes   int  `json:"node_count"`
	} `json:"dht_stats"`
	Nzb struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	} `json:"nzb_config_status"`
}

type dlRate struct {
	Rx int64 `json:"rx_rate"`
	Tx int64 `json:"tx_rate"`
}

// Rates are stored in bytes/s and edited in Ko/s (1 Ko = 1000 octets).
func (r dlRate) RxKo() int64 { return r.Rx / 1000 }
func (r dlRate) TxKo() int64 { return r.Tx / 1000 }

// dlConfig is the downloader configuration. The NNTP password is write-only:
// it is never read, so it can never be shown.
type dlConfig struct {
	MaxTasks    int    `json:"max_downloading_tasks"`
	DownloadDir string `json:"download_dir"`
	WatchDir    string `json:"watch_dir"`
	UseWatchDir bool   `json:"use_watch_dir"`
	DNS1        string `json:"dns1"`
	DNS2        string `json:"dns2"`
	Throttling  struct {
		Normal   dlRate   `json:"normal"`
		Slow     dlRate   `json:"slow"`
		Schedule []string `json:"schedule"`
		Mode     string   `json:"mode"`
	} `json:"throttling"`
	BT struct {
		MaxPeers        int    `json:"max_peers"`
		StopRatio       int    `json:"stop_ratio"`
		Crypto          string `json:"crypto_support"`
		EnableDHT       bool   `json:"enable_dht"`
		EnablePEX       bool   `json:"enable_pex"`
		AnnounceTimeout int    `json:"announce_timeout"`
		MainPort        int    `json:"main_port"`
		DhtPort         int    `json:"dht_port"`
	} `json:"bt"`
	News struct {
		Server      string `json:"server"`
		Port        int    `json:"port"`
		SSL         bool   `json:"ssl"`
		User        string `json:"user"`
		NThreads    int    `json:"nthreads"`
		AutoRepair  bool   `json:"auto_repair"`
		LazyPar2    bool   `json:"lazy_par2"`
		AutoExtract bool   `json:"auto_extract"`
		EraseTmp    bool   `json:"erase_tmp"`
	} `json:"news"`
	Feed struct {
		FetchInterval int `json:"fetch_interval"`
		MaxItems      int `json:"max_items"`
	} `json:"feed"`
}

func (c dlConfig) Dir() string       { return dlDecodePath(c.DownloadDir) }
func (c dlConfig) WatchPath() string { return dlDecodePath(c.WatchDir) }

// Planning is the weekly schedule as 7 days (Monday first) of 24 slots.
func (c dlConfig) Planning() []dlPlanDay {
	days := []string{"Lundi", "Mardi", "Mercredi", "Jeudi", "Vendredi", "Samedi", "Dimanche"}
	out := make([]dlPlanDay, 0, 7)
	for d, name := range days {
		day := dlPlanDay{Name: name}
		for h := 0; h < 24; h++ {
			i := d*24 + h
			slot := "normal"
			if i < len(c.Throttling.Schedule) {
				slot = c.Throttling.Schedule[i]
			}
			day.Slots = append(day.Slots, slotMark(slot))
		}
		out = append(out, day)
	}
	return out
}

type dlPlanDay struct {
	Name  string
	Slots []string
}

func slotMark(s string) string {
	switch s {
	case "slow":
		return "L"
	case "hibernate":
		return "P"
	}
	return "·"
}

// --- pages ---

func (s *Server) downloads(w http.ResponseWriter, r *http.Request) {
	var tasks []dlTask
	err := s.fetch(r.Context(), "downloads/", &tasks)
	var stats dlStats
	var cfg dlConfig
	var feeds []dlFeed
	data := map[string]any{"Tasks": tasks}
	if e := s.fetch(r.Context(), "downloads/stats", &stats); e != nil {
		data["StatsErr"] = e.Error()
	}
	if e := s.fetch(r.Context(), "downloads/config/", &cfg); e != nil {
		data["ConfigErr"] = e.Error()
	} else {
		data["Planning"] = cfg.Planning()
		data["Hours"] = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23}
	}
	if e := s.fetch(r.Context(), "downloads/feeds/", &feeds); e != nil {
		data["FeedsErr"] = e.Error()
	}
	data["Stats"], data["Config"], data["Feeds"] = stats, cfg, feeds
	s.show(w, r, "downloads", data, err)
}

func (s *Server) downloadDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	var t dlTask
	if err := s.fetch(r.Context(), "downloads/"+id, &t); err != nil {
		s.show(w, r, "downloads", map[string]any{"Detail": true}, err)
		return
	}
	data := map[string]any{"Detail": true, "Task": t}
	// Each part may fail on its own (for instance trackers of a non-BitTorrent task).
	var files []dlFile
	if e := s.fetch(r.Context(), "downloads/"+id+"/files", &files); e != nil {
		data["FilesErr"] = e.Error()
	}
	data["Files"] = files
	var log string
	if e := s.fetch(r.Context(), "downloads/"+id+"/log", &log); e == nil {
		data["Log"] = lastLines(log, 60)
	}
	if t.IsBT() {
		var trackers []dlTracker
		var peers []dlPeer
		var pieces string
		var black []dlBlock
		if e := s.fetch(r.Context(), "downloads/"+id+"/trackers", &trackers); e != nil {
			data["TrackersErr"] = e.Error()
		}
		if e := s.fetch(r.Context(), "downloads/"+id+"/peers", &peers); e != nil {
			data["PeersErr"] = e.Error()
		}
		if e := s.fetch(r.Context(), "downloads/"+id+"/pieces", &pieces); e == nil {
			data["PiecesDone"], data["PiecesTotal"] = strings.Count(pieces, "X"), len(pieces)
			if len(pieces) > 4000 {
				pieces = pieces[:4000] + "…"
			}
			data["Pieces"] = pieces
		}
		if e := s.fetch(r.Context(), "downloads/"+id+"/blacklist", &black); e != nil {
			data["BlackErr"] = e.Error()
		}
		data["Trackers"], data["Peers"], data["Blacklist"] = trackers, peers, black
	}
	s.show(w, r, "downloads", data, nil)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (s *Server) feedItems(w http.ResponseWriter, r *http.Request) {
	fid, ok := feedID(w, r)
	if !ok {
		return
	}
	var feed dlFeed
	if err := s.fetch(r.Context(), "downloads/feeds/"+fid, &feed); err != nil {
		s.show(w, r, "downloads", map[string]any{"FeedView": true}, err)
		return
	}
	var items []dlFeedItem
	err := s.fetch(r.Context(), "downloads/feeds/"+fid+"/items/", &items)
	s.show(w, r, "downloads", map[string]any{"FeedView": true, "Feed": feed, "Items": items}, err)
}

// --- task actions ---

func (s *Server) downloadAdd(w http.ResponseWriter, r *http.Request) {
	var urls []string
	for _, l := range strings.Split(r.PostFormValue("urls"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if !validDownloadURL(l) {
			bounce(w, r, "/downloads", "Adresse non prise en charge : "+l+" (http://, https://, ftp:// ou magnet:).")
			return
		}
		urls = append(urls, l)
	}
	if len(urls) == 0 {
		bounce(w, r, "/downloads", "Indique au moins une adresse.")
		return
	}
	// The box wants this one as a form, not JSON.
	body := url.Values{}
	if len(urls) == 1 {
		body.Set("download_url", urls[0])
	} else {
		body.Set("download_url_list", strings.Join(urls, "\n"))
	}
	if dir := formStr(r, "dir"); dir != "" {
		body.Set("download_dir", encodePath(dir))
	}
	if formBool(r, "recursive") {
		body.Set("recursive", "true")
	}
	s.change(w, r, http.MethodPost, "downloads/add", body, "Téléchargement ajouté.", "/downloads")
}

// bounce redirects back with an error message, without touching the box.
func bounce(w http.ResponseWriter, r *http.Request, back, msg string) {
	setFlash(w, "err:"+msg)
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func validDownloadURL(u string) bool {
	if strings.HasPrefix(u, "magnet:?") {
		return true
	}
	p, err := url.Parse(u)
	return err == nil && p.Host != "" && (p.Scheme == "http" || p.Scheme == "https" || p.Scheme == "ftp")
}

// taskID reads the numeric task id of the path, or answers 404.
func taskID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if n, err := strconv.Atoi(id); err != nil || n < 0 {
		http.NotFound(w, r)
		return "", false
	}
	return id, true
}

func (s *Server) downloadStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	st := formStr(r, "status")
	msgs := map[string]string{"stopped": "Téléchargement mis en pause.", "downloading": "Téléchargement repris.",
		"retry": "Nouvel essai lancé.", "seeding": "Partage relancé."}
	msg, known := msgs[st]
	if !known {
		bounce(w, r, "/downloads", "Action inconnue.")
		return
	}
	s.change(w, r, http.MethodPut, "downloads/"+id, map[string]string{"status": st}, msg, dlBackTo(r, id))
}

func (s *Server) downloadPriority(w http.ResponseWriter, r *http.Request) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	p := formStr(r, "io_priority")
	if p != "low" && p != "normal" && p != "high" {
		bounce(w, r, "/downloads", "Priorité inconnue.")
		return
	}
	s.change(w, r, http.MethodPut, "downloads/"+id, map[string]string{"io_priority": p}, "Priorité modifiée.", dlBackTo(r, id))
}

// dlBackTo returns to the detail page when the form came from it (back=detail).
func dlBackTo(r *http.Request, id string) string {
	if r.PostFormValue("back") == "detail" {
		return "/downloads/" + id
	}
	return "/downloads"
}

func (s *Server) downloadDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	if !confirmed(w, r, "/downloads") {
		return
	}
	s.change(w, r, http.MethodDelete, "downloads/"+id, nil, "Téléchargement retiré de la liste (fichiers conservés).", "/downloads")
}

func (s *Server) downloadErase(w http.ResponseWriter, r *http.Request) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	if !confirmed(w, r, "/downloads") {
		return
	}
	s.change(w, r, http.MethodDelete, "downloads/"+id+"/erase", nil, "Téléchargement et fichiers supprimés.", "/downloads")
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	fileID, p := formStr(r, "file"), formStr(r, "priority")
	back := "/downloads/" + id
	if fileID == "" || strings.ContainsAny(fileID, "/\\") || (p != "no_dl" && p != "low" && p != "normal" && p != "high") {
		bounce(w, r, back, "Fichier ou priorité invalide.")
		return
	}
	s.change(w, r, http.MethodPut, "downloads/"+id+"/files/"+url.PathEscape(fileID), map[string]string{"priority": p}, "Priorité du fichier modifiée.", back)
}

func (s *Server) downloadTracker(w http.ResponseWriter, r *http.Request) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	a := formStr(r, "announce")
	if p, err := url.Parse(a); err != nil || p.Host == "" || (p.Scheme != "http" && p.Scheme != "https" && p.Scheme != "udp") {
		bounce(w, r, "/downloads/"+id, "Adresse de tracker invalide (http://, https:// ou udp://).")
		return
	}
	s.change(w, r, http.MethodPost, "downloads/"+id+"/trackers", map[string]string{"announce": a}, "Tracker ajouté.", "/downloads/"+id)
}

func (s *Server) downloadBlock(w http.ResponseWriter, r *http.Request) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	host, secs := formStr(r, "host"), formInt(r, "expire")
	if host == "" || strings.ContainsAny(host, "/\\ ") || secs <= 0 {
		bounce(w, r, "/downloads/"+id, "Indique une adresse IP et une durée en secondes.")
		return
	}
	s.change(w, r, http.MethodPost, "downloads/blacklist", map[string]any{"host": host, "expire": secs}, "Pair bloqué.", "/downloads/"+id)
}

func (s *Server) downloadUnblock(w http.ResponseWriter, r *http.Request) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	host := formStr(r, "host")
	if host == "" || strings.ContainsAny(host, "/\\") || host == "." || host == ".." {
		bounce(w, r, "/downloads/"+id, "Adresse invalide.")
		return
	}
	s.change(w, r, http.MethodDelete, "downloads/blacklist/"+url.PathEscape(host), nil, "Pair débloqué.", "/downloads/"+id)
}

func (s *Server) downloadClearBlacklist(w http.ResponseWriter, r *http.Request) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	s.change(w, r, http.MethodDelete, "downloads/"+id+"/blacklist/empty", nil, "Liste noire vidée.", "/downloads/"+id)
}

// --- feeds ---

func feedID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("fid")
	if n, err := strconv.Atoi(id); err != nil || n < 0 {
		http.NotFound(w, r)
		return "", false
	}
	return id, true
}

func (s *Server) feedAdd(w http.ResponseWriter, r *http.Request) {
	u := formStr(r, "url")
	if p, err := url.Parse(u); err != nil || p.Host == "" || (p.Scheme != "http" && p.Scheme != "https") {
		bounce(w, r, "/downloads", "Adresse de flux invalide (http:// ou https://).")
		return
	}
	s.change(w, r, http.MethodPost, "downloads/feeds/", map[string]string{"url": u}, "Flux ajouté.", "/downloads")
}

func (s *Server) feedFetchAll(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, http.MethodPost, "downloads/feeds/fetch", nil, "Actualisation des flux demandée.", "/downloads")
}

func (s *Server) feedFetch(w http.ResponseWriter, r *http.Request) {
	if fid, ok := feedID(w, r); ok {
		s.change(w, r, http.MethodPost, "downloads/feeds/"+fid+"/fetch", nil, "Actualisation du flux demandée.", "/downloads")
	}
}

func (s *Server) feedAuto(w http.ResponseWriter, r *http.Request) {
	if fid, ok := feedID(w, r); ok {
		s.change(w, r, http.MethodPut, "downloads/feeds/"+fid, map[string]bool{"auto_download": formBool(r, "auto")}, "Flux modifié.", "/downloads")
	}
}

func (s *Server) feedDelete(w http.ResponseWriter, r *http.Request) {
	if fid, ok := feedID(w, r); ok && confirmed(w, r, "/downloads") {
		s.change(w, r, http.MethodDelete, "downloads/feeds/"+fid, nil, "Flux supprimé.", "/downloads")
	}
}

func (s *Server) feedReadAll(w http.ResponseWriter, r *http.Request) {
	if fid, ok := feedID(w, r); ok {
		s.change(w, r, http.MethodPost, "downloads/feeds/"+fid+"/items/mark_all_as_read", nil, "Éléments marqués comme lus.", "/downloads/feeds/"+fid)
	}
}

func (s *Server) feedItemDownload(w http.ResponseWriter, r *http.Request) {
	fid, ok := feedID(w, r)
	iid := r.PathValue("iid")
	if _, err := strconv.Atoi(iid); !ok || err != nil {
		http.NotFound(w, r)
		return
	}
	s.change(w, r, http.MethodPost, "downloads/feeds/"+fid+"/items/"+iid+"/download", nil, "Élément ajouté aux téléchargements.", "/downloads/feeds/"+fid)
}

func (s *Server) feedItemRead(w http.ResponseWriter, r *http.Request) {
	fid, ok := feedID(w, r)
	iid := r.PathValue("iid")
	if _, err := strconv.Atoi(iid); !ok || err != nil {
		http.NotFound(w, r)
		return
	}
	s.change(w, r, http.MethodPut, "downloads/feeds/"+fid+"/items/"+iid, map[string]bool{"is_read": formBool(r, "read")}, "Élément modifié.", "/downloads/feeds/"+fid)
}

// --- settings ---

func (s *Server) downloadsThrottling(w http.ResponseWriter, r *http.Request) {
	m := formStr(r, "mode")
	if m != "normal" && m != "slow" && m != "hibernate" && m != "schedule" {
		bounce(w, r, "/downloads", "Mode inconnu.")
		return
	}
	s.change(w, r, http.MethodPut, "downloads/throttling", map[string]string{"throttling": m}, "Mode de débit modifié.", "/downloads")
}

// currentConfig reads the configuration so a partial change never resets
// the rest of the throttling block.
func (s *Server) currentConfig(r *http.Request) (dlConfig, error) {
	var c dlConfig
	if err := s.fetch(r.Context(), "downloads/config/", &c); err != nil {
		return c, errors.New("lecture de la configuration impossible : " + err.Error())
	}
	return c, nil
}

func throttlingBody(c dlConfig) map[string]any {
	return map[string]any{"throttling": map[string]any{
		"normal":   map[string]int64{"rx_rate": c.Throttling.Normal.Rx, "tx_rate": c.Throttling.Normal.Tx},
		"slow":     map[string]int64{"rx_rate": c.Throttling.Slow.Rx, "tx_rate": c.Throttling.Slow.Tx},
		"schedule": c.Throttling.Schedule,
		"mode":     c.Throttling.Mode,
	}}
}

func (s *Server) downloadsSchedule(w http.ResponseWriter, r *http.Request) {
	day, from, to, mode := formInt(r, "day"), formInt(r, "from"), formInt(r, "to"), formStr(r, "mode")
	if day < -1 || day > 6 || from < 0 || from > 23 || to <= from || to > 24 || (mode != "normal" && mode != "slow" && mode != "hibernate") {
		bounce(w, r, "/downloads", "Créneau invalide : l'heure de fin doit suivre l'heure de début.")
		return
	}
	c, err := s.currentConfig(r)
	if err != nil {
		bounce(w, r, "/downloads", err.Error())
		return
	}
	sched := make([]string, 168)
	copy(sched, c.Throttling.Schedule)
	for i := range sched {
		if sched[i] == "" {
			sched[i] = "normal"
		}
	}
	for d := 0; d < 7; d++ {
		if day != -1 && d != day {
			continue
		}
		for h := from; h < to; h++ {
			sched[d*24+h] = mode
		}
	}
	c.Throttling.Schedule = sched
	s.change(w, r, http.MethodPut, "downloads/config/", throttlingBody(c), "Planning modifié.", "/downloads")
}

func (s *Server) downloadsConfig(w http.ResponseWriter, r *http.Request) {
	crypto := formStr(r, "bt_crypto")
	if crypto != "unsupported" && crypto != "allowed" && crypto != "preferred" && crypto != "required" {
		bounce(w, r, "/downloads", "Mode de chiffrement BitTorrent inconnu.")
		return
	}
	for _, f := range []string{"bt_port", "bt_dht_port", "news_port"} {
		if n := formInt(r, f); n < 0 || n > 65535 {
			bounce(w, r, "/downloads", "Port invalide (0 à 65535).")
			return
		}
	}
	c, err := s.currentConfig(r)
	if err != nil {
		bounce(w, r, "/downloads", err.Error())
		return
	}
	c.Throttling.Normal = dlRate{Rx: int64(formInt(r, "normal_rx")) * 1000, Tx: int64(formInt(r, "normal_tx")) * 1000}
	c.Throttling.Slow = dlRate{Rx: int64(formInt(r, "slow_rx")) * 1000, Tx: int64(formInt(r, "slow_tx")) * 1000}
	body := throttlingBody(c)
	body["max_downloading_tasks"] = formInt(r, "max_tasks")
	body["use_watch_dir"] = formBool(r, "use_watch_dir")
	body["dns1"], body["dns2"] = formStr(r, "dns1"), formStr(r, "dns2")
	if d := formStr(r, "download_dir"); d != "" {
		body["download_dir"] = encodePath(d)
	}
	if d := formStr(r, "watch_dir"); d != "" {
		body["watch_dir"] = encodePath(d)
	}
	body["bt"] = map[string]any{
		"max_peers": formInt(r, "bt_max_peers"), "stop_ratio": formInt(r, "bt_stop_ratio"), "crypto_support": crypto,
		"enable_dht": formBool(r, "bt_dht"), "enable_pex": formBool(r, "bt_pex"), "announce_timeout": formInt(r, "bt_announce"),
		"main_port": formInt(r, "bt_port"), "dht_port": formInt(r, "bt_dht_port"),
	}
	news := map[string]any{
		"server": formStr(r, "news_server"), "port": formInt(r, "news_port"), "ssl": formBool(r, "news_ssl"),
		"user": formStr(r, "news_user"), "nthreads": formInt(r, "news_threads"), "auto_repair": formBool(r, "news_repair"),
		"lazy_par2": formBool(r, "news_lazy"), "auto_extract": formBool(r, "news_extract"), "erase_tmp": formBool(r, "news_erase"),
	}
	// The stored password is never shown; an empty field leaves it unchanged.
	if p := r.PostFormValue("news_password"); p != "" {
		news["password"] = p
	}
	body["news"] = news
	body["feed"] = map[string]any{"fetch_interval": formInt(r, "feed_interval"), "max_items": formInt(r, "feed_max")}
	s.change(w, r, http.MethodPut, "downloads/config/", body, "Configuration enregistrée.", "/downloads")
}
