package server

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"strconv"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// Guest Wi-Fi: the dedicated guest network and its "custom keys" (temporary
// keys that work on the guest network, with or without access to the LAN).

func init() {
	register(module{Slug: "wifi-guest", Title: "Accès invités", Group: "Wi-Fi", Order: 10, Routes: func(s *Server, mux *http.ServeMux) {
		s.page(mux, "GET /wifi-guest", s.wifiGuest)
		s.page(mux, "POST /wifi-guest/config", s.wifiGuestConfig)
		s.page(mux, "POST /wifi-guest/keys", s.wifiGuestAdd)
		s.page(mux, "POST /wifi-guest/keys/{id}/delete", s.wifiGuestDelete)
	}})
	keyID := ops.Param{Name: "id", Type: "integer", Description: "Custom key id, from wifi_guest_key_list"}
	ops.Register(
		ops.Op{Name: "wifi_guest_config_get", Description: "Get the guest Wi-Fi network settings: ssid, ssid_read_only, hide_ssid, encryption.",
			Path: "wifi/custom_keys/config/"},
		ops.Op{Name: "wifi_guest_config_set", Description: "Rename the guest Wi-Fi network (ssid). Guests must reconnect.", Method: http.MethodPut,
			Path: "wifi/custom_keys/config/", Body: true, BodyHelp: `{"ssid": "my-guest-network"}`},
		ops.Op{Name: "wifi_guest_key_list", Description: "List the guest custom keys: id, remaining seconds (0 = never expires), params (description, key in clear, max_use_count, duration, access_type full or net_only) and the hosts that used it.",
			Path: "wifi/custom_key/"},
		ops.Op{Name: "wifi_guest_key_get", Description: "Get one guest custom key, key included.",
			Path: "wifi/custom_key/{id}", Params: []ops.Param{keyID}},
		ops.Op{Name: "wifi_guest_key_add", Description: "Create a guest custom key. access_type full gives the guest access to your local network, net_only isolates them on the internet only.",
			Method: http.MethodPost, Path: "wifi/custom_key/", Body: true, Dangerous: true,
			BodyHelp: `{"description": "friends", "key": "at least 8 characters", "max_use_count": 10, "duration": 86400, "access_type": "net_only"} (max_use_count 0 = unlimited)`},
		ops.Op{Name: "wifi_guest_key_delete", Description: "Delete a guest custom key. The stations connected with it are disconnected.",
			Method: http.MethodDelete, Path: "wifi/custom_key/{id}", Params: []ops.Param{keyID}, Dangerous: true},
	)
}

type wifiGuestKey struct {
	ID        int   `json:"id"`
	Remaining int64 `json:"remaining"`
	Params    struct {
		Description string `json:"description"`
		Key         string `json:"key"`
		MaxUse      int    `json:"max_use_count"`
		Duration    int    `json:"duration"`
		Access      string `json:"access_type"`
	} `json:"params"`
	Users []struct {
		Hostname string `json:"hostname"`
	} `json:"users"`
}

// AccessLabel says what the guest can reach.
func (k wifiGuestKey) AccessLabel() string {
	if k.Params.Access == "full" {
		return "réseau local + internet"
	}
	return "internet seulement"
}

func (k wifiGuestKey) KeyHidden() bool { return k.Params.Key == "" || k.Params.Key == "****" }

func (s *Server) wifiGuest(w http.ResponseWriter, r *http.Request) {
	var cfg struct {
		SSID     string `json:"ssid"`
		ReadOnly bool   `json:"ssid_read_only"`
		Hidden   bool   `json:"hide_ssid"`
		Enc      string `json:"encryption"`
	}
	var keys []wifiGuestKey
	err := s.fetch(r.Context(), "wifi/custom_keys/config/", &cfg)
	if e := s.fetch(r.Context(), "wifi/custom_key/", &keys); err == nil {
		err = e
	}
	s.show(w, r, "wifi-guest", map[string]any{"Cfg": cfg, "EncLabel": wifiEncryptionLabel(cfg.Enc), "Keys": keys}, err)
}

func (s *Server) wifiGuestConfig(w http.ResponseWriter, r *http.Request) {
	ssid := formStr(r, "ssid")
	if ssid == "" || len(ssid) > 32 {
		setFlash(w, "err:Le nom du réseau fait de 1 à 32 octets.")
		http.Redirect(w, r, "/wifi-guest", http.StatusSeeOther)
		return
	}
	s.change(w, r, http.MethodPut, "wifi/custom_keys/config/", map[string]string{"ssid": ssid}, "Nom du réseau invités enregistré.", "/wifi-guest")
}

const keyAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789" // gitleaks:allow (character set, not a secret)

// randomKey makes a readable random key.
func randomKey(n int) (string, error) {
	b := make([]byte, n)
	for i := range b {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(len(keyAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = keyAlphabet[j.Int64()]
	}
	return string(b), nil
}

func (s *Server) wifiGuestAdd(w http.ResponseWriter, r *http.Request) {
	back := "/wifi-guest"
	desc, key, access := formStr(r, "description"), r.PostFormValue("key"), formStr(r, "access_type")
	maxUse, duration := formInt(r, "max_use_count"), formInt(r, "duration")
	switch {
	case access != "full" && access != "net_only":
		setFlash(w, "err:Type d'accès invalide.")
	case maxUse < 0 || maxUse > 127 || duration < 0:
		setFlash(w, "err:Nombre d'appareils (0 à 127) ou durée invalide.")
	case key != "" && (len(key) < 8 || len(key) > 63):
		setFlash(w, "err:La clé fait de 8 à 63 caractères.")
	case access == "full" && !confirmed(w, r, back):
		return
	default:
		if key == "" {
			var err error
			if key, err = randomKey(16); err != nil {
				setFlash(w, "err:Génération de la clé impossible : "+err.Error())
				break
			}
		}
		s.change(w, r, http.MethodPost, "wifi/custom_key/", map[string]any{"description": desc, "key": key, "max_use_count": maxUse,
			"duration": duration, "access_type": access}, "Accès invité créé : ouvre « Clé » dans la liste.", back)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (s *Server) wifiGuestDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := wifiID(w, r, "id", "/wifi-guest")
	if !ok || !confirmed(w, r, "/wifi-guest") {
		return
	}
	s.change(w, r, http.MethodDelete, "wifi/custom_key/"+strconv.Itoa(id), nil, "Accès invité supprimé.", "/wifi-guest")
}
