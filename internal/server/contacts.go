package server

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Contacts: the address book stored on the box, with its phone numbers,
// e-mails, postal addresses and URLs. Contact data is personal: it is shown
// but never logged (only ids go in paths).
//
// The documentation lists no contact groups endpoint besides the group_id
// filter of the list, so groups have no page.

// relatedKind describes one kind of object attached to a contact. The box
// handles all of them the same way: contact/{id}/<plural>/ to list, and
// <singular>/ and <singular>/{id}/ to create, read, update and delete.
type relatedKind struct {
	Singular string
	Label    string   // for operation descriptions
	Fields   []string // writable fields besides contact_id and type
	Types    []string
}

var relatedKinds = map[string]relatedKind{
	"numbers":   {"number", "phone number", []string{"number"}, []string{"fixed", "mobile", "work", "fax", "other"}},
	"emails":    {"email", "e-mail address", []string{"email"}, []string{"home", "work", "other"}},
	"addresses": {"address", "postal address", []string{"number", "street", "street2", "zipcode", "city", "country"}, []string{"home", "work", "other"}},
	"urls":      {"url", "URL", []string{"url"}, []string{"profile", "blog", "site", "other"}},
}

func init() {
	register(module{Slug: "contacts", Title: "Contacts", Group: "Téléphonie", Order: 10, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /contacts", s.contacts)
		s.page(mux, "POST /contacts/add", s.addContact)
		s.page(mux, "POST /contacts/{id}/edit", s.editContact)
		s.page(mux, "POST /contacts/{id}/delete", s.deleteContact)
		s.page(mux, "POST /contacts/{id}/{kind}/add", s.addRelated)
		s.page(mux, "POST /contacts/{id}/{kind}/{rid}/delete", s.deleteRelated)
	}})
	id := ops.Param{Name: "id", Description: "Contact id (integer)"}
	ops.Register(
		ops.Op{Name: "contact_list", Description: "List the contacts: id, display_name, first_name, last_name, company, notes, numbers, emails, addresses, urls. Use limit -1 for all of them.",
			Path: "contact/", Params: []ops.Param{
				{Name: "start", Type: "integer", Description: "Offset", Query: true},
				{Name: "limit", Type: "integer", Description: "Number of contacts to return, -1 for no limit", Query: true},
				{Name: "group_id", Type: "integer", Description: "Only the contacts of this group", Query: true}}},
		ops.Op{Name: "contact_get", Description: "Get one contact with its numbers, e-mails, addresses and URLs.", Path: "contact/{id}/", Params: []ops.Param{id}},
		ops.Op{Name: "contact_create", Description: "Create a contact; add numbers, e-mails, addresses and URLs afterwards with the contact_*_create operations.", Method: http.MethodPost,
			Path: "contact/", Body: true, BodyHelp: `{"display_name": "Sandy Kilo", "first_name": "Sandy", "last_name": "Kilo", "company": "", "notes": ""}`},
		ops.Op{Name: "contact_update", Description: "Update a contact's names, company or notes.", Method: http.MethodPut,
			Path: "contact/{id}/", Params: []ops.Param{id}, Body: true, BodyHelp: `{"company": "Freebox"}`},
		ops.Op{Name: "contact_delete", Description: "Delete a contact and everything attached to it. It cannot be recovered.", Method: http.MethodDelete,
			Path: "contact/{id}/", Params: []ops.Param{id}, Dangerous: true},
	)
	examples := map[string]string{
		"numbers":   `{"contact_id": 9, "type": "mobile", "number": "0639980011"}`,
		"emails":    `{"contact_id": 9, "type": "home", "email": "sandy@example.org"}`,
		"addresses": `{"contact_id": 9, "type": "home", "number": "11", "street": "rue du Pont", "zipcode": "75008", "city": "Paris", "country": "France"}`,
		"urls":      `{"contact_id": 9, "type": "site", "url": "https://example.org"}`,
	}
	for plural, k := range relatedKinds {
		itemID := ops.Param{Name: "id", Description: k.Label + " id (integer)"}
		ops.Register(
			ops.Op{Name: "contact_" + plural + "_list", Description: "List the " + k.Label + "s of a contact.",
				Path: "contact/{contact_id}/" + plural + "/", Params: []ops.Param{{Name: "contact_id", Description: "Contact id (integer)"}}},
			ops.Op{Name: "contact_" + k.Singular + "_get", Description: "Get one " + k.Label + " by its own id.",
				Path: k.Singular + "/{id}/", Params: []ops.Param{itemID}},
			ops.Op{Name: "contact_" + k.Singular + "_create", Description: "Add a " + k.Label + " to a contact (type is one of " + strings.Join(k.Types, ", ") + ").", Method: http.MethodPost,
				Path: k.Singular + "/", Body: true, BodyHelp: examples[plural]},
			ops.Op{Name: "contact_" + k.Singular + "_update", Description: "Update a " + k.Label + ".", Method: http.MethodPut,
				Path: k.Singular + "/{id}/", Params: []ops.Param{itemID}, Body: true, BodyHelp: examples[plural]},
			ops.Op{Name: "contact_" + k.Singular + "_delete", Description: "Delete a " + k.Label + ".", Method: http.MethodDelete,
				Path: k.Singular + "/{id}/", Params: []ops.Param{itemID}},
		)
	}
}

type contactNumber struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	Number    string `json:"number"`
	IsDefault bool   `json:"is_default"`
	IsOwn     bool   `json:"is_own"`
}

type contactEmail struct {
	ID    int    `json:"id"`
	Type  string `json:"type"`
	Email string `json:"email"`
}

type contactAddress struct {
	ID      int    `json:"id"`
	Type    string `json:"type"`
	Number  string `json:"number"`
	Street  string `json:"street"`
	Street2 string `json:"street2"`
	Zipcode string `json:"zipcode"`
	City    string `json:"city"`
	Country string `json:"country"`
}

// Line is the address on one line.
func (a contactAddress) Line() string {
	var parts []string
	for _, p := range []string{strings.TrimSpace(a.Number + " " + a.Street), a.Street2, strings.TrimSpace(a.Zipcode + " " + a.City), a.Country} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

type contactURL struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

type contact struct {
	ID          int              `json:"id"`
	DisplayName string           `json:"display_name"`
	FirstName   string           `json:"first_name"`
	LastName    string           `json:"last_name"`
	Company     string           `json:"company"`
	Notes       string           `json:"notes"`
	Numbers     []contactNumber  `json:"numbers"`
	Emails      []contactEmail   `json:"emails"`
	Addresses   []contactAddress `json:"addresses"`
	URLs        []contactURL     `json:"urls"`
}

// Name is the best name to show for the contact.
func (c contact) Name() string {
	for _, n := range []string{c.DisplayName, strings.TrimSpace(c.FirstName + " " + c.LastName), c.Company} {
		if n != "" {
			return n
		}
	}
	return "Sans nom"
}

// digits keeps only the digits of a phone number, to compare 06 12… and 0612….
func digits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

func (c contact) matches(q string) bool {
	text := strings.ToLower(c.Name() + " " + c.FirstName + " " + c.LastName + " " + c.Company)
	for _, e := range c.Emails {
		text += " " + strings.ToLower(e.Email)
	}
	if strings.Contains(text, q) {
		return true
	}
	if d := digits(q); d != "" {
		for _, n := range c.Numbers {
			if strings.Contains(digits(n.Number), d) {
				return true
			}
		}
	}
	return false
}

func (s *Server) contacts(w http.ResponseWriter, r *http.Request) {
	var all []contact
	err := s.fetch(r.Context(), "contact/?start=0&limit=-1", &all)
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	shown := all[:0]
	for _, c := range all {
		if q == "" || c.matches(q) {
			shown = append(shown, c)
		}
	}
	sort.Slice(shown, func(i, j int) bool { return strings.ToLower(shown[i].Name()) < strings.ToLower(shown[j].Name()) })
	s.show(w, r, "contacts", map[string]any{
		"Contacts": shown, "Total": len(all), "Q": q,
		"NumberTypes": relatedKinds["numbers"].Types, "HomeTypes": relatedKinds["emails"].Types, "URLTypes": relatedKinds["urls"].Types,
	}, err)
}

// contactBody is the writable part of a contact, from the form. A missing
// display name is made from the other names.
func contactBody(r *http.Request) map[string]string {
	b := map[string]string{
		"first_name": formStr(r, "first_name"), "last_name": formStr(r, "last_name"),
		"company": formStr(r, "company"), "notes": formStr(r, "notes"), "display_name": formStr(r, "display_name"),
	}
	if b["display_name"] == "" {
		b["display_name"] = strings.TrimSpace(b["first_name"] + " " + b["last_name"])
	}
	if b["display_name"] == "" {
		b["display_name"] = b["company"]
	}
	return b
}

func (s *Server) addContact(w http.ResponseWriter, r *http.Request) {
	body := contactBody(r)
	if body["display_name"] == "" {
		setFlash(w, "err:Donne au moins un nom, un prénom ou une société.")
		http.Redirect(w, r, "/contacts", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var created struct {
		ID int `json:"id"`
	}
	if err := s.box.Do(ctx, http.MethodPost, "contact/", body, &created); err != nil {
		s.log.Error("change", "method", http.MethodPost, "path", "contact/", "err", err)
		setFlash(w, "err:"+err.Error())
		http.Redirect(w, r, "/contacts", http.StatusSeeOther)
		return
	}
	s.log.Info("change", "method", http.MethodPost, "path", "contact/")
	// An optional first phone number saves a second trip to the form.
	if num := formStr(r, "number"); num != "" && created.ID > 0 {
		nb := map[string]any{"contact_id": created.ID, "type": formStr(r, "type"), "number": num}
		if err := s.box.Do(ctx, http.MethodPost, "number/", nb, nil); err != nil {
			s.log.Error("change", "method", http.MethodPost, "path", "number/", "err", err)
			setFlash(w, "err:Contact créé, mais le numéro n'a pas été ajouté : "+err.Error())
			http.Redirect(w, r, "/contacts", http.StatusSeeOther)
			return
		}
	}
	setFlash(w, "ok:Contact ajouté.")
	http.Redirect(w, r, "/contacts", http.StatusSeeOther)
}

func (s *Server) editContact(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	body := contactBody(r)
	if body["display_name"] == "" {
		setFlash(w, "err:Donne au moins un nom, un prénom ou une société.")
		http.Redirect(w, r, "/contacts", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "contact/"+id+"/", body, "Contact modifié.", "/contacts")
}

func (s *Server) deleteContact(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok || !confirmed(w, r, "/contacts") {
		return
	}
	s.change(w, r, http.MethodDelete, "contact/"+id+"/", nil, "Contact supprimé.", "/contacts")
}

func (s *Server) addRelated(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	kind, known := relatedKinds[r.PathValue("kind")]
	if !ok || !known {
		if ok {
			http.NotFound(w, r)
		}
		return
	}
	cid, _ := strconv.Atoi(id)
	body := map[string]any{"contact_id": cid, "type": formStr(r, "type")}
	filled := false
	for _, f := range kind.Fields {
		v := formStr(r, f)
		body[f] = v
		filled = filled || v != ""
	}
	if !filled {
		setFlash(w, "err:Le champ est vide.")
		http.Redirect(w, r, "/contacts", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPost, kind.Singular+"/", body, "Ajouté au contact.", "/contacts")
}

func (s *Server) deleteRelated(w http.ResponseWriter, r *http.Request) {
	rid, ok := pathID(w, r, "rid")
	kind, known := relatedKinds[r.PathValue("kind")]
	if !ok || !known {
		if ok {
			http.NotFound(w, r)
		}
		return
	}
	s.change(w, r, http.MethodDelete, kind.Singular+"/"+rid+"/", nil, "Retiré du contact.", "/contacts")
}

// pathID returns a numeric path value, or answers 404.
func pathID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	v := r.PathValue(name)
	if n, err := strconv.Atoi(v); err != nil || n < 0 {
		http.NotFound(w, r)
		return "", false
	}
	return url.PathEscape(v), true
}
