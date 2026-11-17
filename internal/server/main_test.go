package server

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	bcryptCost = bcrypt.MinCost // every test signs up: keep -race runs short
	os.Exit(m.Run())
}
