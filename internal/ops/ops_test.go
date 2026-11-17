package ops

import (
	"context"
	"errors"
	"testing"
)

type recBox struct{ path string }

func (b *recBox) Do(_ context.Context, _, path string, _, _ any) error {
	b.path = path
	return nil
}

func TestCallPathArguments(t *testing.T) {
	op := Op{Name: "x", Description: "x", Path: "lan/browser/{interface}/{id}/",
		Params: []Param{{Name: "interface"}, {Name: "id"}, {Name: "limit", Query: true}}}
	b := &recBox{}
	if _, err := Call(context.Background(), b, op, map[string]any{"interface": "pub", "id": "ether-00:11:22:33:44:55", "limit": float64(5)}); err != nil {
		t.Fatal(err)
	}
	if b.path != "lan/browser/pub/ether-00:11:22:33:44:55/?limit=5" {
		t.Fatalf("path = %q", b.path)
	}
	for _, bad := range []string{"..", ".", "", "a/b", `a\b`} {
		if _, err := Call(context.Background(), b, op, map[string]any{"interface": "pub", "id": bad}); err == nil {
			t.Errorf("id %q accepted", bad)
		}
	}
	if _, err := Call(context.Background(), b, Op{Name: "r", Description: "r", Path: "system/reboot/", Dangerous: true}, map[string]any{}); !errors.Is(err, ErrConfirm) {
		t.Fatalf("dangerous without confirm: %v", err)
	}
}
