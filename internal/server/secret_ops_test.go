package server

import (
	"regexp"
	"testing"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// TestSecretOps locks the reads that return keys or passwords: read-only
// API tokens may not call them, and MCP does not announce them read-only.
func TestSecretOps(t *testing.T) {
	for _, name := range []string{
		"wifi_bss_list", "wifi_bss_get", "wifi_bss_default", "wifi_guest_key_list", "wifi_guest_key_get",
		"vpn_config_download", "vpn_client_configs_list", "vpn_client_config_get",
		"vpn_users_list", "vpn_user_get", "vpn_server_config_get", "wifi_defaults_get", "files_share_list",
	} {
		op, ok := ops.Get(name)
		if !ok {
			t.Errorf("%s: not registered", name)
			continue
		}
		if !op.Secret {
			t.Errorf("%s: must be marked Secret", name)
		}
	}
}

// TestReadsMentioningSecrets catches a new read whose description admits it
// returns a key or a password but that is not marked Secret.
func TestReadsMentioningSecrets(t *testing.T) {
	words := regexp.MustCompile(`(?i)\b(in clear|private key|wpa key|keys? (is|are) returned|returns? the (key|password))\b`)
	for _, op := range ops.All() {
		if op.ReadOnly() && !op.Secret && words.MatchString(op.Description) {
			t.Errorf("%s reads a secret (%q) but is not marked Secret", op.Name, words.FindString(op.Description))
		}
	}
}
