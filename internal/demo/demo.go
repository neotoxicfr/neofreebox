// Package demo is a fake Freebox with invented data, for trying the interface
// without a box (NEOFREEBOX_DEMO=true). Every address, name and number is
// fictional: documentation IP ranges, locally administered MACs, and phone
// numbers reserved for fiction by the ARCEP.
package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/neotoxicfr/neofreebox/internal/fbx"
)

// Box answers the reads with fixed fictional data, accepts every write
// without doing anything, and generates the RRD series on demand.
type Box struct {
	// Now is the clock the relative timestamps are computed from.
	Now func() time.Time
}

// New returns a demo box.
func New() *Box { return &Box{Now: time.Now} }

// Get fills out with the fictional answer for path.
func (b *Box) Get(_ context.Context, path string, out any) error {
	return b.read(path, out)
}

// Do reads like Get, reads the RRD with a POST, and accepts any other write.
func (b *Box) Do(_ context.Context, method, path string, in, out any) error {
	switch {
	case method == http.MethodGet:
		return b.read(path, out)
	case method == http.MethodPost && strings.HasPrefix(path, "rrd/"):
		return fill(b.rrd(in), out)
	}
	if out != nil {
		_ = fill(M{}, out) // a write answers success; a typed result stays zero
	}
	return nil
}

// Permissions grants every right, so no page is hidden.
func (b *Box) Permissions(context.Context) (map[string]bool, error) {
	return map[string]bool{"settings": true, "calls": true, "contacts": true, "downloader": true,
		"explorer": true, "parental": true, "pvr": true, "player": true, "profile": true,
		"camera": true, "home": true, "tv": true, "wdo": true, "vm": false}, nil
}

// Raw serves a small fake file for the download buttons.
func (b *Box) Raw(_ context.Context, path string) (*http.Response, error) {
	body := "Fichier fictif du mode démonstration (" + path + ").\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/plain; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func (b *Box) read(path string, out any) error {
	v, ok := b.lookup(strings.TrimSuffix(path, "?removeHidden=true"))
	if !ok {
		return &fbx.Error{Status: http.StatusNotFound, Code: "invalid_request", Msg: "Pas de données de démonstration pour " + path}
	}
	return fill(v, out)
}

// fill copies v into out through JSON, as the real client does.
func fill(v, out any) error {
	if out == nil {
		return nil
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.NewDecoder(bytes.NewReader(buf)).Decode(out)
}
