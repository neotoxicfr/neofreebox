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
