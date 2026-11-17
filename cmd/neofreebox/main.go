// Command neofreebox is a self-hosted control panel for the Freebox.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/neotoxicfr/neofreebox/internal/fbx"
)

// version is set at build time.
var version = "dev"

// appID must never change: the box ties the app token to it.
const appID = "io.github.neotoxicfr.neofreebox"

const usage = `usage: neofreebox <command>

  pair -o FILE   ask the box for an app token (confirm on the box), save it to FILE
  check          open a session with the token and print the box and its rights

Environment:
  NEOFREEBOX_BOX_URL         box URL (default ` + fbx.DefaultURL + `)
  NEOFREEBOX_APP_TOKEN_FILE  file holding the app token
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var err error
	switch os.Args[1] {
	case "pair":
		err = pair(ctx, os.Args[2:])
	case "check":
		err = check(ctx)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "neofreebox:", err)
		os.Exit(1)
	}
}

func app() fbx.App {
	host, _ := os.Hostname()
	if host == "" {
		host = "server"
	}
	return fbx.App{ID: appID, Name: "neofreebox", Version: version, Device: host}
}

func pair(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	out := fs.String("o", "", "file the app token is written to (must not exist)")
	_ = fs.Parse(args)
	if *out == "" {
		return errors.New("pair: -o FILE is required")
	}
	// Created before asking, so a bad path fails before the box is involved.
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("pair: %w", err)
	}
	defer f.Close()
	c, err := fbx.New(os.Getenv("NEOFREEBOX_BOX_URL"), app(), "")
	if err != nil {
		return err
	}
	token, id, err := c.Authorize(ctx)
	if err != nil {
		_ = os.Remove(*out)
		return fmt.Errorf("pair: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Request sent: confirm « neofreebox » on the box…")
	for {
		st, err := c.AuthorizationStatus(ctx, id)
		if err != nil {
			_ = os.Remove(*out)
			return fmt.Errorf("pair: %w", err)
		}
		switch st {
		case fbx.StatusPending:
			select {
			case <-ctx.Done():
				_ = os.Remove(*out)
				return ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		case fbx.StatusGranted:
			if _, err := f.WriteString(token + "\n"); err != nil {
				return fmt.Errorf("pair: write token: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Granted. Token saved to %s; keep it secret.\n", *out)
			return nil
		default:
			_ = os.Remove(*out)
			return fmt.Errorf("pair: request %s", st)
		}
	}
}

func check(ctx context.Context) error {
	path := os.Getenv("NEOFREEBOX_APP_TOKEN_FILE")
	if path == "" {
		return errors.New("check: NEOFREEBOX_APP_TOKEN_FILE is not set")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("check: %w", err)
	}
	c, err := fbx.New(os.Getenv("NEOFREEBOX_BOX_URL"), app(), strings.TrimSpace(string(b)))
	if err != nil {
		return err
	}
	defer func() { _ = c.Logout(context.WithoutCancel(ctx)) }()
	var sys struct {
		FirmwareVersion string `json:"firmware_version"`
		ModelInfo       struct {
			PrettyName string `json:"pretty_name"`
		} `json:"model_info"`
	}
	if err := c.Get(ctx, "system/", &sys); err != nil {
		return fmt.Errorf("check: %w", err)
	}
	perms, err := c.Permissions(ctx)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(perms))
	for k, v := range perms {
		if v {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	fmt.Printf("%s, firmware %s\nrights: %s\n", sys.ModelInfo.PrettyName, sys.FirmwareVersion, strings.Join(names, ", "))
	return nil
}
