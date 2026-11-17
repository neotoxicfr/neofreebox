package server

import (
	"net/http"
	"net/netip"
	"strconv"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// LAN: the box's own network settings (router or bridge mode, IP, names)
// and the static routes.

func init() {
	register(module{Slug: "lan", Title: "Réseau local", Group: "Réseau", Order: 40, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /lan", s.lan)
		s.page(mux, "POST /lan/names", s.lanNames)
		s.page(mux, "POST /lan/network", s.lanNetwork)
		s.page(mux, "POST /lan/routes/add", s.lanRouteAdd)
		s.page(mux, "POST /lan/routes/{index}/delete", s.lanRouteDelete)
	}})
	ops.Register(
		ops.Op{Name: "lan_config_get", Description: "Get the box LAN settings: mode (router/bridge), ip, name, name_dns, name_mdns, name_netbios, local_domain.",
			Path: "lan/config/"},
		ops.Op{Name: "lan_config_update", Description: "Change the box LAN settings. Bridge mode disables most box services and changing ip moves the box to another address: both can lock you out.",
			Method: http.MethodPut, Path: "lan/config/", Body: true, Dangerous: true,
			BodyHelp: `{"mode": "router", "ip": "192.168.1.254", "local_domain": "home.arpa", "name": "Freebox Server", "name_dns": "freebox-server", "name_mdns": "Freebox-Server", "name_netbios": "Freebox_Server"}`},
		ops.Op{Name: "lan_routes_get", Description: "List the static routes: prefix (CIDR), gateway, enabled, description.",
			Path: "lan/routes/"},
		ops.Op{Name: "lan_routes_update", Description: "Replace the whole list of static routes (the body is a JSON array; send the existing routes you want to keep).",
			Method: http.MethodPut, Path: "lan/routes/", Body: true, BodyArray: true, Dangerous: true,
			BodyHelp: `[{"prefix": "192.168.42.0/24", "gateway": "192.168.1.38", "enabled": true, "description": "My route"}]`},
	)
}

type lanRoute struct {
	Prefix      string `json:"prefix"`
	Gateway     string `json:"gateway"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

func (s *Server) lan(w http.ResponseWriter, r *http.Request) {
	var cfg map[string]any
	var routes []lanRoute
	err := s.fetch(r.Context(), "lan/config/", &cfg)
	if err == nil {
		err = s.fetch(r.Context(), "lan/routes/", &routes)
	}
	str := func(k string) string { v, _ := cfg[k].(string); return v }
	s.show(w, r, "lan", map[string]any{
		"Mode": str("mode"), "IP": str("ip"), "Name": str("name"), "LocalDomain": str("local_domain"),
		"NameDNS": str("name_dns"), "NameMDNS": str("name_mdns"), "NameNetbios": str("name_netbios"), "Routes": routes,
	}, err)
}

// lanUpdate writes the LAN config with the given fields changed. The box
// takes the whole object, so the current one is read first.
func (s *Server) lanUpdate(w http.ResponseWriter, r *http.Request, set map[string]any, done string) {
	var cfg map[string]any
	if err := s.fetch(r.Context(), "lan/config/", &cfg); err != nil {
		setFlash(w, "err:"+humanError(err))
		http.Redirect(w, r, "/lan", http.StatusSeeOther)
		return
	}
	for k, v := range set {
		cfg[k] = v
	}
	s.change(w, r, http.MethodPut, "lan/config/", cfg, done, "/lan")
}

func (s *Server) lanNames(w http.ResponseWriter, r *http.Request) {
	set := map[string]any{}
	for _, k := range []string{"name", "name_dns", "name_mdns", "name_netbios", "local_domain"} {
		v := formStr(r, k)
		if v == "" || len(v) > 63 {
			setFlash(w, "err:Chaque nom est obligatoire et fait 63 caractères au plus.")
			http.Redirect(w, r, "/lan", http.StatusSeeOther)
			return
		}
		set[k] = v
	}
	s.lanUpdate(w, r, set, "Noms enregistrés.")
}

// lanNetwork changes the mode and the IP of the box: either can lock the user out.
func (s *Server) lanNetwork(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/lan") {
		return
	}
	mode := formStr(r, "mode")
	ip, err := netip.ParseAddr(formStr(r, "ip"))
	if (mode != "router" && mode != "bridge") || err != nil || !ip.Is4() {
		setFlash(w, "err:Mode ou adresse IPv4 invalide.")
		http.Redirect(w, r, "/lan", http.StatusSeeOther)
		return
	}
	s.lanUpdate(w, r, map[string]any{"mode": mode, "ip": ip.String()}, "Réseau local enregistré.")
}

func (s *Server) putRoutes(w http.ResponseWriter, r *http.Request, routes []lanRoute, done string) {
	if routes == nil {
		routes = []lanRoute{}
	}
	s.change(w, r, http.MethodPut, "lan/routes/", routes, done, "/lan")
}

func (s *Server) lanRouteAdd(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/lan") {
		return
	}
	prefix, perr := netip.ParsePrefix(formStr(r, "prefix"))
	gw, gerr := netip.ParseAddr(formStr(r, "gateway"))
	if perr != nil || gerr != nil || !prefix.Addr().Is4() || !gw.Is4() {
		setFlash(w, "err:Préfixe (ex. 192.168.42.0/24) ou passerelle IPv4 invalide.")
		http.Redirect(w, r, "/lan", http.StatusSeeOther)
		return
	}
	var routes []lanRoute
	if err := s.fetch(r.Context(), "lan/routes/", &routes); err != nil {
		setFlash(w, "err:"+humanError(err))
		http.Redirect(w, r, "/lan", http.StatusSeeOther)
		return
	}
	routes = append(routes, lanRoute{Prefix: prefix.String(), Gateway: gw.String(), Enabled: true, Description: formStr(r, "description")})
	s.putRoutes(w, r, routes, "Route ajoutée.")
}

func (s *Server) lanRouteDelete(w http.ResponseWriter, r *http.Request) {
	if !confirmed(w, r, "/lan") {
		return
	}
	i, err := strconv.Atoi(r.PathValue("index"))
	var routes []lanRoute
	if err == nil {
		err = s.fetch(r.Context(), "lan/routes/", &routes)
	}
	if err != nil || i < 0 || i >= len(routes) {
		setFlash(w, "err:Route introuvable.")
		http.Redirect(w, r, "/lan", http.StatusSeeOther)
		return
	}
	s.putRoutes(w, r, append(routes[:i], routes[i+1:]...), "Route supprimée.")
}
