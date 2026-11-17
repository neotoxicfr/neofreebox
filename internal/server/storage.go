package server

import (
	"net/http"
	"strconv"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Storage: disks, partitions, power management and RAID arrays.

func init() {
	register(module{Slug: "storage", Title: "Disques", Group: "Système", Order: 50, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /storage", s.storage)
		s.page(mux, "POST /storage/disks/{id}/state", s.diskState)
		s.page(mux, "POST /storage/disks/{id}/format", s.diskFormat)
		s.page(mux, "POST /storage/partitions/{id}/state", s.partitionState)
		s.page(mux, "POST /storage/partitions/{id}/check", s.partitionCheck)
		s.page(mux, "POST /storage/config", s.storageConfig)
	}})
	id := func(what string) []ops.Param { return []ops.Param{{Name: "id", Type: "integer", Description: what}} }
	ops.Register(
		ops.Op{Name: "storage_disks_list", Description: "List the disks (internal, usb, sata, nvme) with model, serial, temperature, state, read/write error counters and their partitions (label, fstype, state, total/used/free bytes).",
			Path: "storage/disk/"},
		ops.Op{Name: "storage_disk_get", Description: "Get one disk with its partitions.", Path: "storage/disk/{id}", Params: id("Disk id")},
		ops.Op{Name: "storage_disk_update", Description: "Enable or disable (eject) a disk; disabling unmounts its partitions.", Method: http.MethodPut,
			Path: "storage/disk/{id}", Params: id("Disk id"), Body: true, BodyHelp: `{"state": "disabled"}`, Dangerous: true},
		ops.Op{Name: "storage_disk_fsadvice", Description: "Check a disk and get formatting advice (recommended fstype, table_type, reason, partitions that would be deleted).",
			Path: "storage/disk/{disk_id}/fsadvice", Params: []ops.Param{
				{Name: "disk_id", Type: "integer", Description: "Disk id"},
				{Name: "partition_id", Type: "integer", Description: "Partition to check; leave out for an empty disk", Query: true},
				{Name: "dedicated_disk", Type: "boolean", Description: "True if the disk is used only with the Freebox", Query: true}}},
		ops.Op{Name: "storage_disk_format", Description: "Format a disk with one partition using all the space. ALL DATA ON THE DISK IS LOST.", Method: http.MethodPut,
			Path: "storage/disk/{id}/format/", Params: id("Disk id"), Body: true,
			BodyHelp: `{"table_type": "gpt", "fs_type": "ext4", "label": "usb-disk"}`, Dangerous: true},
		ops.Op{Name: "storage_partitions_list", Description: "List every partition: id, disk_id, label, fstype, state, fsck_result, total/used/free bytes, mount path (base64).",
			Path: "storage/partition/"},
		ops.Op{Name: "storage_partition_get", Description: "Get one partition.", Path: "storage/partition/{id}", Params: id("Partition id")},
		ops.Op{Name: "storage_partition_update", Description: "Mount or unmount a partition.", Method: http.MethodPut,
			Path: "storage/partition/{id}", Params: id("Partition id"), Body: true, BodyHelp: `{"state": "umounted"}`, Dangerous: true},
		ops.Op{Name: "storage_partition_check", Description: "Check a partition's file system (ro = read only, rw = try to repair: the partition is unmounted meanwhile); follow it with operation_pct on the partition.", Method: http.MethodPut,
			Path: "storage/partition/{id}/check/", Params: id("Partition id"), Body: true, BodyHelp: `{"checkmode": "ro"}`, Dangerous: true},
		ops.Op{Name: "storage_config_get", Description: "Get the external disk power management settings (external_pm_enabled, external_pm_idle_before_spindown in minutes).",
			Path: "storage/config/"},
		ops.Op{Name: "storage_config_update", Description: "Change the external disk power management settings.", Method: http.MethodPut,
			Path: "storage/config/", Body: true, BodyHelp: `{"external_pm_enabled": true, "external_pm_idle_before_spindown": 10}`},
		ops.Op{Name: "storage_raid_list", Description: "List the RAID arrays: level, state, degraded, sync_action, sync_completed_percent, members (role, disk model, temperature).",
			Path: "storage/raid/"},
		ops.Op{Name: "storage_raid_get", Description: "Get one RAID array.", Path: "storage/raid/{id}", Params: id("Array id")},
		ops.Op{Name: "storage_raid_create", Description: "Create a RAID array from internal disks. Members are formatted: data is lost.", Method: http.MethodPost,
			Path: "storage/raid/", Body: true, BodyHelp: `{"level": "raid1", "name": "Freebox", "members": [{"id": 1000}, {"id": 2000}]}`, Dangerous: true},
		ops.Op{Name: "storage_raid_delete", Description: "Destroy a RAID array. Data is lost.", Method: http.MethodDelete,
			Path: "storage/raid/{id}", Params: id("Array id"), Dangerous: true},
		ops.Op{Name: "storage_raid_update", Description: "Start or stop a RAID array (state running or stopped); other fields are ignored.", Method: http.MethodPut,
			Path: "storage/raid/{id}", Params: id("Array id"), Body: true, BodyHelp: `{"id": 0, "state": "stopped"}`, Dangerous: true},
		ops.Op{Name: "storage_raid_forcestart", Description: "Force start an incomplete array in degraded mode (only when its state is error).", Method: http.MethodPost,
			Path: "storage/raid/{id}/forcestart", Params: id("Array id"), Dangerous: true},
		ops.Op{Name: "storage_raid_faulty_remove", Description: "Remove the faulty members of a stopped RAID array.", Method: http.MethodDelete,
			Path: "storage/raid/{id}/members/faulty", Params: id("Array id"), Dangerous: true},
		ops.Op{Name: "storage_raid_members_add", Description: "Add members to an array that has missing members, so it rebuilds.", Method: http.MethodPut,
			Path: "storage/raid/{id}/members", Params: id("Array id"), Body: true, BodyHelp: `{"members": [{"id": 3000}]}`, Dangerous: true},
		ops.Op{Name: "storage_raid_addspares", Description: "Re-add out-of-sync members that appear as spares.", Method: http.MethodPost,
			Path: "storage/raid/{id}/members/addspares", Params: id("Array id"), Dangerous: true},
	)
}

type partition struct {
	ID          int    `json:"id"`
	DiskID      int    `json:"disk_id"`
	State       string `json:"state"`
	FSType      string `json:"fstype"`
	Label       string `json:"label"`
	TotalBytes  int64  `json:"total_bytes"`
	UsedBytes   int64  `json:"used_bytes"`
	FreeBytes   int64  `json:"free_bytes"`
	FsckResult  string `json:"fsck_result"`
	OperationPc struct {
		Percent int `json:"percent"`
	} `json:"operation_pct"`
}

// StateLabel is the partition state in French.
func (p partition) StateLabel() string {
	return label(p.State, map[string]string{"mounted": "montée", "umounted": "démontée", "checking": "vérification en cours",
		"formatting": "formatage en cours", "mounting": "montage en cours", "umounting": "démontage en cours", "ejecting": "éjection en cours",
		"error": "erreur"})
}

// FsckLabel is the last check result in French.
func (p partition) FsckLabel() string {
	return label(p.FsckResult, map[string]string{"fs_clean": "aucune erreur", "clean": "aucune erreur", "fs_corrected": "erreurs corrigées",
		"corrected": "erreurs corrigées", "fs_uncorrected": "erreurs non corrigées", "uncorrected": "erreurs non corrigées",
		"no_run_yet": "jamais vérifiée"})
}

// label translates a raw state, keeping the raw value visible when unknown.
func label(raw string, names map[string]string) string {
	if l, ok := names[raw]; ok {
		return l
	}
	if raw == "" {
		return ""
	}
	return "inconnu (" + raw + ")"
}

// Badge is the badge modifier of the state: on, warn, off or none.
func (p partition) Badge() string {
	switch {
	case p.State == "mounted":
		return " on"
	case p.State == "error":
		return " off"
	case p.Busy():
		return " warn"
	}
	return ""
}

// UsedPct is the used share of the partition, 0 to 100.
func (p partition) UsedPct() int {
	if p.TotalBytes <= 0 {
		return 0
	}
	return int(p.UsedBytes * 100 / p.TotalBytes)
}

// Busy tells whether an operation (check, format, mount…) is in progress.
func (p partition) Busy() bool {
	switch p.State {
	case "checking", "formatting", "mounting", "umounting", "ejecting":
		return true
	}
	return false
}

type disk struct {
	ID           int         `json:"id"`
	Type         string      `json:"type"`
	State        string      `json:"state"`
	Model        string      `json:"model"`
	Serial       string      `json:"serial"`
	Firmware     string      `json:"firmware"`
	TableType    string      `json:"table_type"`
	TotalBytes   int64       `json:"total_bytes"`
	Temp         int         `json:"temp"`
	Spinning     bool        `json:"spinning"`
	ReadErrors   int64       `json:"read_error_requests"`
	WriteErrors  int64       `json:"write_error_requests"`
	Partitions   []partition `json:"partitions"`
	OperationPct struct {
		Percent int `json:"percent"`
	} `json:"operation_pct"`
}

// StateLabel is the disk state in French.
func (d disk) StateLabel() string {
	return label(d.State, map[string]string{"enabled": "activé", "disabled": "désactivé (éjecté)", "formatting": "formatage en cours",
		"error": "erreur"})
}

// Badge is the badge modifier of the state: on, warn, off or none.
func (d disk) Badge() string {
	switch d.State {
	case "enabled":
		return " on"
	case "error":
		return " off"
	case "formatting":
		return " warn"
	}
	return ""
}

// TypeLabel is the disk type in French.
func (d disk) TypeLabel() string {
	return label(d.Type, map[string]string{"internal": "interne", "usb": "USB", "sata": "SATA", "nvme": "NVMe", "raid": "RAID"})
}

// Internal disks cannot be formatted nor ejected from here.
func (d disk) Internal() bool { return d.Type == "internal" }

// Errors says whether the disk reported failed requests, a sign of failure.
func (d disk) Errors() bool { return d.ReadErrors > 0 || d.WriteErrors > 0 }

// StateLabel is the array state in French.
func (a raidArray) StateLabel() string {
	l := label(a.State, map[string]string{"running": "en marche", "stopped": "arrêté", "starting": "démarrage en cours",
		"stopping": "arrêt en cours", "error": "erreur", "creating": "création en cours"})
	if a.Degraded {
		l += ", dégradé"
	}
	return l
}

// Badge is the badge modifier of the state: on, warn, off or none.
func (a raidArray) Badge() string {
	switch {
	case a.State == "error":
		return " off"
	case a.Degraded || a.State == "starting" || a.State == "stopping" || a.State == "creating":
		return " warn"
	case a.State == "running":
		return " on"
	}
	return ""
}

// SyncLabel is the running synchronisation in French.
func (a raidArray) SyncLabel() string {
	return label(a.SyncAction, map[string]string{"resync": "synchronisation", "recover": "reconstruction", "check": "vérification",
		"repair": "réparation", "reshape": "réorganisation", "frozen": "suspendue"})
}

// RoleLabel is the member's role in French.
func (m raidMember) RoleLabel() string {
	return label(m.Role, map[string]string{"member": "membre", "spare": "de secours", "faulty": "défaillant", "missing": "manquant",
		"sync": "en synchronisation"})
}

type raidMember struct {
	ID   int    `json:"id"`
	Role string `json:"role"`
	Disk struct {
		Model string `json:"model"`
		Temp  int    `json:"temp"`
	} `json:"disk"`
	CorrectedReadErrors int `json:"corrected_read_errors"`
}

type raidArray struct {
	ID          int          `json:"id"`
	Name        string       `json:"name"`
	Level       string       `json:"level"`
	State       string       `json:"state"`
	Degraded    bool         `json:"degraded"`
	SyncAction  string       `json:"sync_action"`
	SyncPercent int          `json:"sync_completed_percent"`
	ArraySize   int64        `json:"array_size"`
	Members     []raidMember `json:"members"`
}

type storageConfig struct {
	PMEnabled bool `json:"external_pm_enabled"`
	PMIdle    int  `json:"external_pm_idle_before_spindown"`
}

func (s *Server) storage(w http.ResponseWriter, r *http.Request) {
	var disks []disk
	err := s.fetch(r.Context(), "storage/disk/", &disks)
	var cfg storageConfig
	if err == nil {
		err = s.fetch(r.Context(), "storage/config/", &cfg)
	}
	// The RAID API is unstable and absent on boxes without RAID: ignore its errors.
	var raids []raidArray
	if err == nil {
		if e := s.fetch(r.Context(), "storage/raid/", &raids); e != nil {
			s.log.Debug("storage: raid unavailable", "err", e)
			raids = nil
		}
	}
	s.show(w, r, "storage", map[string]any{"Disks": disks, "Config": cfg, "Raids": raids}, err)
}

// storageID reads a numeric {id} from the URL; anything else is refused.
func storageID(w http.ResponseWriter, r *http.Request, back string) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 0 {
		setFlash(w, "err:Identifiant invalide.")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return 0, false
	}
	return id, true
}

func (s *Server) diskState(w http.ResponseWriter, r *http.Request) {
	id, ok := storageID(w, r, "/storage")
	if !ok {
		return
	}
	state := formStr(r, "state")
	switch state {
	case "enabled":
	case "disabled":
		if !confirmed(w, r, "/storage") {
			return
		}
	default:
		setFlash(w, "err:État de disque inconnu.")
		http.Redirect(w, r, "/storage", http.StatusSeeOther)
		return
	}
	done := "Disque activé."
	if state == "disabled" {
		done = "Disque éjecté."
	}
	s.change(w, r, http.MethodPut, "storage/disk/"+strconv.Itoa(id), map[string]string{"state": state}, done, "/storage")
}

func (s *Server) diskFormat(w http.ResponseWriter, r *http.Request) {
	id, ok := storageID(w, r, "/storage")
	if !ok {
		return
	}
	if !confirmed(w, r, "/storage") {
		return
	}
	table, fs := formStr(r, "table_type"), formStr(r, "fs_type")
	if (table != "gpt" && table != "msdos") || (fs != "ext4" && fs != "vfat" && fs != "exfat" && fs != "ntfs") {
		setFlash(w, "err:Table de partitions ou système de fichiers non pris en charge.")
		http.Redirect(w, r, "/storage", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "storage/disk/"+strconv.Itoa(id)+"/format/",
		map[string]string{"table_type": table, "fs_type": fs, "label": formStr(r, "label")},
		"Formatage lancé.", "/storage")
}

func (s *Server) partitionState(w http.ResponseWriter, r *http.Request) {
	id, ok := storageID(w, r, "/storage")
	if !ok {
		return
	}
	state := formStr(r, "state")
	switch state {
	case "mounted":
	case "umounted":
		if !confirmed(w, r, "/storage") {
			return
		}
	default:
		setFlash(w, "err:État de partition inconnu.")
		http.Redirect(w, r, "/storage", http.StatusSeeOther)
		return
	}
	done := "Partition montée."
	if state == "umounted" {
		done = "Partition démontée."
	}
	s.change(w, r, http.MethodPut, "storage/partition/"+strconv.Itoa(id), map[string]string{"state": state}, done, "/storage")
}

func (s *Server) partitionCheck(w http.ResponseWriter, r *http.Request) {
	id, ok := storageID(w, r, "/storage")
	if !ok {
		return
	}
	mode := formStr(r, "checkmode")
	if mode != "ro" && mode != "rw" {
		mode = "ro"
	}
	// Repairing unmounts the partition: services that use it stop.
	if mode == "rw" && !confirmed(w, r, "/storage") {
		return
	}
	s.change(w, r, http.MethodPut, "storage/partition/"+strconv.Itoa(id)+"/check/", map[string]string{"checkmode": mode},
		"Vérification lancée.", "/storage")
}

func (s *Server) storageConfig(w http.ResponseWriter, r *http.Request) {
	idle := formInt(r, "idle")
	if idle < 1 || idle > 1440 {
		setFlash(w, "err:Le délai doit être compris entre 1 et 1440 minutes.")
		http.Redirect(w, r, "/storage", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "storage/config/",
		map[string]any{"external_pm_enabled": formBool(r, "enabled"), "external_pm_idle_before_spindown": idle},
		"Gestion de l'énergie enregistrée.", "/storage")
}
