package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/neotoxicfr/neofreebox/internal/fbx"
)

func TestReadOnlyRefusesWrites(t *testing.T) {
	b := readOnly{nil} // a write must be refused before the client is used
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if err := b.Do(context.Background(), m, "system/reboot/", nil, nil); !fbx.IsCode(err, "read_only") {
			t.Errorf("%s: err = %v, want read_only", m, err)
		}
	}
}

func TestEnvBool(t *testing.T) {
	for v, want := range map[string]bool{"": false, "true": true, "1": true, "false": false, " TRUE ": true} {
		t.Setenv("NB_TEST", v)
		if got, err := envBool("NB_TEST"); err != nil || got != want {
			t.Errorf("%q: %v, %v", v, got, err)
		}
	}
	for _, v := range []string{"tru", "yes", "on"} {
		t.Setenv("NB_TEST", v)
		if _, err := envBool("NB_TEST"); err == nil {
			t.Errorf("%q accepted", v)
		}
	}
}

func TestReadOnlyRefusesWritingGet(t *testing.T) {
	b := readOnly{nil}
	if err := b.Get(context.Background(), "vpn/download_config/openvpn_routed/u", nil); !fbx.IsCode(err, "read_only") {
		t.Fatalf("err = %v, want read_only", err)
	}
}
