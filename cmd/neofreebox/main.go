// Command neofreebox is a self-hosted control panel for the Freebox.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/neotoxicfr/neofreebox/internal/demo"
	"github.com/neotoxicfr/neofreebox/internal/fbx"
	"github.com/neotoxicfr/neofreebox/internal/server"
)

// version is set at build time.
var version = "dev"

// appID must never change: the box ties the app token to it.
const appID = "io.github.neotoxicfr.neofreebox"

const usage = `usage: neofreebox <command>

  serve          run the web interface
  pair -o FILE   ask the box for an app token (confirm on the box), save it to FILE
  check          open a session with the token and print the box and its rights
  get PATH       print the JSON result of GET /api/v16/PATH (read-only, for debugging)
  healthcheck    exit 0 if the local server answers (for the container)

Environment:
  NEOFREEBOX_ADDR            listen address (default :8080)
  NEOFREEBOX_DATA_DIR        where the account is kept (default ./data)
  NEOFREEBOX_BOX_URL         box URL (default ` + fbx.DefaultURL + `)
  NEOFREEBOX_APP_TOKEN_FILE  file holding the app token
  NEOFREEBOX_READ_ONLY       true to refuse every write to the box (demos, audits)
  NEOFREEBOX_DEMO            true to run on a built-in fake box with fictional data: no token,
                             no box needed; sign in as demo / demo-neofreebox (data in a temporary
                             directory, deleted on exit: never NEOFREEBOX_DATA_DIR)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(ctx)
	case "pair":
		err = pair(ctx, os.Args[2:])
	case "check":
		err = check(ctx)
	case "get":
		err = get(ctx, os.Args[2:])
	case "healthcheck":
		err = healthcheck(ctx)
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

// client builds a box client from the token file.
func client() (*fbx.Client, error) {
	path := os.Getenv("NEOFREEBOX_APP_TOKEN_FILE")
	if path == "" {
		return nil, errors.New("NEOFREEBOX_APP_TOKEN_FILE is not set")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return nil, fmt.Errorf("%s is empty", path)
	}
	return fbx.New(os.Getenv("NEOFREEBOX_BOX_URL"), app(), token)
}

func serve(ctx context.Context) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	demoMode, _ := strconv.ParseBool(os.Getenv("NEOFREEBOX_DEMO"))
	var c *fbx.Client
	var box server.Box
	if demoMode {
		box = demo.New()
		log.Warn("demo mode: fictional data, no box is contacted", "user", server.DemoUser, "password", server.DemoPassword)
	} else {
		var err error
		if c, err = client(); err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		box = c
	}
	dir := envOr("NEOFREEBOX_DATA_DIR", "./data")
	if demoMode {
		// The demo account has a public password: it must never land in a
		// data directory that a real instance could reuse.
		tmp, err := os.MkdirTemp("", "neofreebox-demo-")
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		dir = tmp
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	server.Version = version
	if ro, _ := strconv.ParseBool(os.Getenv("NEOFREEBOX_READ_ONLY")); ro && c != nil {
		box = readOnly{c}
		log.Warn("read-only mode: no write is sent to the box")
	}
	s, err := server.New(box, dir, log)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	if demoMode {
		if err := s.EnableDemo(); err != nil {
			return fmt.Errorf("serve: %w", err)
		}
	}
	srv := &http.Server{
		Addr:              envOr("NEOFREEBOX_ADDR", ":8080"),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		if c != nil {
			_ = c.Logout(sctx)
		}
	}()
	log.Info("listening", "addr", srv.Addr, "version", version)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func get(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("get: one API path expected, e.g. lan/browser/pub/")
	}
	c, err := client()
	if err != nil {
		return fmt.Errorf("get: %w", err)
	}
	defer func() { _ = c.Logout(context.WithoutCancel(ctx)) }()
	var out json.RawMessage
	if err := c.Get(ctx, args[0], &out); err != nil {
		return fmt.Errorf("get: %w", err)
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, out, "", "  ") != nil {
		pretty.Write(out)
	}
	fmt.Println(pretty.String())
	return nil
}

func healthcheck(ctx context.Context) error {
	addr := envOr("NEOFREEBOX_ADDR", ":8080")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: %s", resp.Status)
	}
	return nil
}

// readOnly lets reads through and refuses every write, for demos, tests and
// audits against a real box. rrd/ is read with a POST, so it passes.
type readOnly struct{ *fbx.Client }

func (b readOnly) Do(ctx context.Context, method, path string, in, out any) error {
	// vpn/download_config is a GET that regenerates an OpenVPN user's file.
	read := method == http.MethodGet && !strings.HasPrefix(path, "vpn/download_config")
	if read || (method == http.MethodPost && strings.HasPrefix(path, "rrd/")) {
		return b.Client.Do(ctx, method, path, in, out)
	}
	return &fbx.Error{Status: http.StatusForbidden, Code: "read_only", Msg: "mode lecture seule : rien n'est envoyé à la box"}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func check(ctx context.Context) error {
	c, err := client()
	if err != nil {
		return fmt.Errorf("check: %w", err)
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
