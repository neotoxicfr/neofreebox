package server

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Files: browse the box's disks (fs/ls), manage files through fs tasks and
// share links. Paths travel base64-encoded, as the box returns them.
//
// Not covered by the page: downloading (dl/ returns raw bytes, but the Box
// interface only decodes JSON envelopes) and uploading (WebSocket only).

func init() {
	register(module{Slug: "files", Title: "Fichiers", Group: "Contenus", Order: 10, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /files", s.files)
		s.page(mux, "POST /files/mkdir", s.fileMkdir)
		s.page(mux, "POST /files/rename", s.fileRename)
		s.page(mux, "POST /files/move", s.fileMove)
		s.page(mux, "POST /files/copy", s.fileCopy)
		s.page(mux, "POST /files/archive", s.fileArchive)
		s.page(mux, "POST /files/delete", s.fileDelete)
		s.page(mux, "POST /files/share", s.shareCreate)
		s.page(mux, "POST /files/share/{token}/delete", s.shareDelete)
		s.page(mux, "POST /files/tasks/{id}/delete", s.taskDelete)
	}})
	p := func(what string) ops.Param {
		return ops.Param{Name: "path", Description: what + " (base64, as returned by files_ls)"}
	}
	q := func(name, what string) ops.Param {
		return ops.Param{Name: name, Type: "boolean", Description: what, Query: true}
	}
	taskID := []ops.Param{{Name: "id", Type: "integer", Description: "Task id"}}
	ops.Register(
		ops.Op{Name: "files_tasks_list", Description: "List the file system tasks (cp, mv, rm, archive, extract, hash…): id, type, state, error, progress, src, dst.",
			Path: "fs/tasks/"},
		ops.Op{Name: "files_task_get", Description: "Get one file system task.", Path: "fs/tasks/{id}", Params: taskID},
		ops.Op{Name: "files_task_update", Description: "Pause or resume a file system task (state paused or running).", Method: http.MethodPut,
			Path: "fs/tasks/{id}", Params: taskID, Body: true, BodyHelp: `{"state": "paused"}`},
		ops.Op{Name: "files_task_delete", Description: "Delete a file system task, and stop it if it runs. Files already processed are left as is.", Method: http.MethodDelete,
			Path: "fs/tasks/{id}", Params: taskID},
		ops.Op{Name: "files_task_hash", Description: "Get the hash computed by a finished hash task.", Path: "fs/tasks/{id}/hash", Params: taskID},
		ops.Op{Name: "files_ls", Description: "List a folder. Each entry has path (base64, reuse it for the next calls), name, type (dir or file), size, modification, mimetype. The root is Lw== (base64 of /).",
			Path: "fs/ls/{path}", Params: []ops.Param{p("Folder path"),
				q("onlyFolder", "Only list folders"), q("countSubFolder", "Count files and subfolders of folders"), q("removeHidden", "Leave out hidden files"),
				{Name: "limit", Type: "integer", Description: "Maximum number of entries", Query: true},
				{Name: "cursor", Description: "Cursor returned by the previous call, to continue", Query: true}}},
		ops.Op{Name: "files_info", Description: "Get the information of one file or folder.", Path: "fs/info/{path}", Params: []ops.Param{p("File path")}},
		ops.Op{Name: "files_info_batch", Description: "Get the information of several paths at once; invalid paths are ignored.", Method: http.MethodPost,
			Path: "fs/info", Body: true, BodyHelp: `["L0Rpc3F1ZSBkdXIvUGhvdG9z"]  (a JSON array of base64 paths)`},
		ops.Op{Name: "files_move", Description: "Start a task that moves files. mode resolves name conflicts: overwrite, both, recent or skip.", Method: http.MethodPost,
			Path: "fs/mv/", Body: true, BodyHelp: `{"files": ["<base64 path>"], "dst": "<base64 folder>", "mode": "both"}`},
		ops.Op{Name: "files_copy", Description: "Start a task that copies files. mode resolves name conflicts: overwrite, both, recent or skip.", Method: http.MethodPost,
			Path: "fs/cp/", Body: true, BodyHelp: `{"files": ["<base64 path>"], "dst": "<base64 folder>", "mode": "both"}`},
		ops.Op{Name: "files_remove", Description: "Start a task that deletes files and folders for good.", Method: http.MethodPost,
			Path: "fs/rm/", Body: true, BodyHelp: `{"files": ["<base64 path>"]}`, Dangerous: true},
		ops.Op{Name: "files_cat", Description: "Start a task that concatenates files into dst, optionally deleting the sources or overwriting dst.", Method: http.MethodPost,
			Path: "fs/cat/", Body: true, Dangerous: true,
			BodyHelp: `{"files": ["<base64 path>"], "dst": "<base64 path>", "multi_volumes": false, "delete_files": false, "overwrite": false, "append": true}`},
		ops.Op{Name: "files_archive", Description: "Start a task that creates an archive (the extension of dst, such as .zip, sets the format).", Method: http.MethodPost,
			Path: "fs/archive/", Body: true, BodyHelp: `{"files": ["<base64 path>"], "dst": "<base64 path of the archive>"}`},
		ops.Op{Name: "files_extract", Description: "Start a task that extracts an archive into a folder.", Method: http.MethodPost,
			Path: "fs/extract/", Body: true, Dangerous: true,
			BodyHelp: `{"src": "<base64 archive>", "dst": "<base64 folder>", "password": "", "delete_archive": false, "overwrite": false}`},
		ops.Op{Name: "files_repair", Description: "Start a task that repairs files from a .par2 file.", Method: http.MethodPost,
			Path: "fs/repair/", Body: true, BodyHelp: `{"src": "<base64 .par2 path>", "delete_archive": false}`},
		ops.Op{Name: "files_hash", Description: "Start a task that hashes a file; read the result with files_task_hash once the task is done.", Method: http.MethodPost,
			Path: "fs/hash/", Body: true, BodyHelp: `{"src": "<base64 path>", "hash_type": "sha1"}`},
		ops.Op{Name: "files_mkdir", Description: "Create a folder (immediate, no task).", Method: http.MethodPost,
			Path: "fs/mkdir/", Body: true, BodyHelp: `{"parent": "<base64 folder>", "dirname": "New folder"}`},
		ops.Op{Name: "files_rename", Description: "Rename a file or folder (immediate); dst is the new name in clear text, without path. Returns the new base64 path.", Method: http.MethodPost,
			Path: "fs/rename/", Body: true, BodyHelp: `{"src": "<base64 path>", "dst": "new name.txt"}`},
		ops.Op{Name: "files_share_list", Description: "List the share links: token, path, name, expire (unix time, 0 = never), fullurl (empty if remote HTTP access is off).",
			Path: "share_link/"},
		ops.Op{Name: "files_share_get", Description: "Get one share link.", Path: "share_link/{token}", Params: []ops.Param{{Name: "token", Description: "Share link token"}}},
		ops.Op{Name: "files_share_create", Description: "Create a share link to a file or folder: anyone with the URL can read it from the internet when remote HTTP access is on.", Method: http.MethodPost,
			Path: "share_link/", Body: true, Dangerous: true, BodyHelp: `{"path": "<base64 path>", "expire": 1893456000, "fullurl": ""}`},
		ops.Op{Name: "files_share_delete", Description: "Delete a share link.", Method: http.MethodDelete,
			Path: "share_link/{token}", Params: []ops.Param{{Name: "token", Description: "Share link token"}}},
		ops.Op{Name: "files_uploads_list", Description: "List the uploads (progress tracking only; sending files needs the WebSocket API).", Path: "upload/"},
		ops.Op{Name: "files_upload_get", Description: "Track one upload: size, uploaded, status.", Path: "upload/{id}", Params: []ops.Param{{Name: "id", Type: "integer", Description: "Upload id"}}},
		ops.Op{Name: "files_upload_cancel", Description: "Cancel an upload that is in progress.", Method: http.MethodDelete,
			Path: "upload/{id}/cancel", Params: []ops.Param{{Name: "id", Type: "integer", Description: "Upload id"}}},
		ops.Op{Name: "files_upload_delete", Description: "Delete an upload record, closing its connection if needed.", Method: http.MethodDelete,
			Path: "upload/{id}", Params: []ops.Param{{Name: "id", Type: "integer", Description: "Upload id"}}},
	)
}

const rootPath = "Lw==" // base64 of "/"

type fileEntry struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Size         int64  `json:"size"`
	Modification int64  `json:"modification"`
	Hidden       bool   `json:"hidden"`
}

func (f fileEntry) IsDir() bool { return f.Type == "dir" }

type shareLink struct {
	Token   string `json:"token"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Expire  int64  `json:"expire"`
	FullURL string `json:"fullurl"`
}

type fsTask struct {
	ID       int    `json:"id"`
	Type     string `json:"type"`
	State    string `json:"state"`
	Error    string `json:"error"`
	Progress int    `json:"progress"`
	Created  int64  `json:"created_ts"`
}

type crumb struct{ Name, P string }

func enc(p string) string { return base64.StdEncoding.EncodeToString([]byte(p)) }

// decodePath checks a base64 path from a request: it must be an absolute
// path, in canonical base64.
func decodePath(p string) (string, bool) {
	b, err := base64.StdEncoding.DecodeString(p)
	if err != nil || len(b) == 0 || b[0] != '/' || strings.ContainsRune(string(b), 0) {
		return "", false
	}
	return string(b), true
}

// backTo is where forms return: the folder they were submitted from.
func backTo(r *http.Request) string {
	if p := r.PostFormValue("p"); p != "" {
		if _, ok := decodePath(p); ok {
			return "/files?p=" + url.QueryEscape(p)
		}
	}
	return "/files"
}

func fail(w http.ResponseWriter, r *http.Request, msg string) {
	setFlash(w, "err:"+msg)
	http.Redirect(w, r, backTo(r), http.StatusSeeOther)
}

// selection reads the checked files, refusing anything that is not a path.
func selection(r *http.Request) ([]string, bool) {
	_ = r.ParseForm()
	files := r.PostForm["files"]
	for _, f := range files {
		if _, ok := decodePath(f); !ok {
			return nil, false
		}
	}
	return files, len(files) > 0
}

func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	cur, ok := decodePath(p)
	if !ok {
		p, cur = rootPath, "/"
	}
	var res struct {
		Entries []fileEntry `json:"entries"`
	}
	err := s.fetch(r.Context(), "fs/ls/"+p+"?removeHidden=true", &res)
	entries := res.Entries[:0]
	for _, e := range res.Entries {
		if !e.Hidden && e.Name != "." && e.Name != ".." {
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	crumbs := []crumb{{"Racine", rootPath}}
	acc := ""
	for _, seg := range strings.Split(strings.Trim(cur, "/"), "/") {
		if seg != "" {
			acc += "/" + seg
			crumbs = append(crumbs, crumb{seg, enc(acc)})
		}
	}
	// Share links and tasks are secondary: the page still works without them.
	var shares []shareLink
	var tasks []fsTask
	if err == nil {
		if e := s.fetch(r.Context(), "share_link/", &shares); e != nil {
			s.log.Debug("files: share links unavailable", "err", e)
		}
		if e := s.fetch(r.Context(), "fs/tasks/", &tasks); e != nil {
			s.log.Debug("files: tasks unavailable", "err", e)
		}
	}
	s.show(w, r, "files", map[string]any{"Entries": entries, "Crumbs": crumbs, "P": p, "Cur": cur, "Shares": shares, "Tasks": tasks}, err)
}

// cleanName refuses names that would escape the folder.
func cleanName(n string) bool {
	return n != "" && n != "." && n != ".." && !strings.ContainsAny(n, "/\x00")
}

func (s *Server) fileMkdir(w http.ResponseWriter, r *http.Request) {
	parent, name := r.PostFormValue("p"), formStr(r, "name")
	if _, ok := decodePath(parent); !ok || !cleanName(name) {
		fail(w, r, "Nom de dossier invalide.")
		return
	}
	s.change(w, r, http.MethodPost, "fs/mkdir/", map[string]string{"parent": parent, "dirname": name}, "Dossier créé.", backTo(r))
}

func (s *Server) fileRename(w http.ResponseWriter, r *http.Request) {
	src, name := r.PostFormValue("src"), formStr(r, "name")
	if _, ok := decodePath(src); !ok || !cleanName(name) {
		fail(w, r, "Nom invalide.")
		return
	}
	s.change(w, r, http.MethodPost, "fs/rename/", map[string]string{"src": src, "dst": name}, "Renommé.", backTo(r))
}

func (s *Server) transfer(w http.ResponseWriter, r *http.Request, endpoint, done string) {
	files, ok := selection(r)
	dest := path.Clean("/" + strings.TrimSpace(r.PostFormValue("dest")))
	mode := formStr(r, "mode")
	switch mode {
	case "overwrite":
		if !confirmed(w, r, backTo(r)) {
			return
		}
	case "both", "recent", "skip":
	default:
		mode, ok = "", false
	}
	if !ok || strings.Contains(r.PostFormValue("dest"), "..") {
		fail(w, r, "Choisis des fichiers, un dossier de destination et un mode de conflit.")
		return
	}
	s.change(w, r, http.MethodPost, endpoint, map[string]any{"files": files, "dst": enc(dest), "mode": mode}, done, backTo(r))
}

func (s *Server) fileMove(w http.ResponseWriter, r *http.Request) {
	s.transfer(w, r, "fs/mv/", "Déplacement lancé.")
}

func (s *Server) fileCopy(w http.ResponseWriter, r *http.Request) {
	s.transfer(w, r, "fs/cp/", "Copie lancée.")
}

func (s *Server) fileArchive(w http.ResponseWriter, r *http.Request) {
	files, ok := selection(r)
	cur, okCur := decodePath(r.PostFormValue("p"))
	name := formStr(r, "archive")
	if !ok || !okCur || !cleanName(name) {
		fail(w, r, "Choisis des fichiers et un nom d'archive (par exemple archive.zip).")
		return
	}
	s.change(w, r, http.MethodPost, "fs/archive/", map[string]any{"files": files, "dst": enc(path.Join(cur, name))},
		"Archive en cours de création.", backTo(r))
}

func (s *Server) fileDelete(w http.ResponseWriter, r *http.Request) {
	files, ok := selection(r)
	if !ok {
		fail(w, r, "Choisis au moins un fichier.")
		return
	}
	if !confirmed(w, r, backTo(r)) {
		return
	}
	s.change(w, r, http.MethodPost, "fs/rm/", map[string]any{"files": files}, "Suppression lancée.", backTo(r))
}

func (s *Server) shareCreate(w http.ResponseWriter, r *http.Request) {
	target := r.PostFormValue("target")
	if _, ok := decodePath(target); !ok {
		fail(w, r, "Fichier invalide.")
		return
	}
	if !confirmed(w, r, backTo(r)) {
		return
	}
	var expire int64
	if days := formInt(r, "days"); days > 0 {
		expire = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	}
	s.change(w, r, http.MethodPost, "share_link/", map[string]any{"path": target, "expire": expire, "fullurl": ""},
		"Lien de partage créé.", backTo(r))
}

func (s *Server) shareDelete(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !cleanName(token) {
		fail(w, r, "Lien invalide.")
		return
	}
	s.change(w, r, http.MethodDelete, "share_link/"+url.PathEscape(token), nil, "Lien de partage supprimé.", backTo(r))
}

func (s *Server) taskDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 0 {
		fail(w, r, "Tâche invalide.")
		return
	}
	s.change(w, r, http.MethodDelete, "fs/tasks/"+strconv.Itoa(id), nil, "Tâche retirée.", backTo(r))
}
