package server

import (
	"testing"

	"github.com/neotoxicfr/neofreebox/internal/ops"
)

// TestSecretOps locks the reads that return keys or passwords: read-only
// API tokens may not call them, and MCP does not announce them read-only.
func TestSecretOps(t *testing.T) {
	for _, name := range []string{
		"wifi_bss_list", "wifi_bss_get", "wifi_bss_default", "wifi_guest_key_list", "wifi_guest_key_get",
		"vpn_config_download", "vpn_client_configs_list", "vpn_client_config_get",
		"vpn_users_list", "vpn_user_get", "vpn_server_config_get",
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
