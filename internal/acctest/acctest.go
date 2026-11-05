// Package acctest runs a real Harbor Pilot server for acceptance tests.
//
// Start builds harbor-pilot from the application checkout ($HP_APP_DIR,
// default ../go-harbor-pilot next to this repository) or uses a prebuilt
// binary ($HP_APP_BIN), starts `serve` on a free loopback port with a
// temporary data directory and a 0600 master key file, creates the first
// administrator through the one-time setup flow, and issues an API token for
// a service principal with every configuration permission. The provider
// reads the server URL and token from HP_SERVER and HP_API_TOKEN.
package acctest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
)

// Server is a running harbor-pilot process.
type Server struct {
	URL   string
	Token string
	// Client acts like an operator outside Terraform (managed_by=api).
	Client *client.Client
	// TF is a client that identifies as Terraform.
	TF *client.Client

	cmd     *exec.Cmd
	dir     string
	bin     string
	env     []string
	logFile *os.File
}

// Permissions granted to the test service principal.
var Permissions = []string{"config.read", "config.write", "policy.publish", "secrets.write", "tokens.manage", "usage.read",
	"activity.read", "events.manage", "notifications.manage", "compat.manage", "audit.read", "users.manage", "auth.settings", "system.manage"}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// Start launches the server. Call Stop when done.
func Start(ctx context.Context) (*Server, error) {
	dir, err := os.MkdirTemp("", "hp-acc-")
	if err != nil {
		return nil, err
	}
	s := &Server{dir: dir}
	if err := s.start(ctx); err != nil {
		s.Stop()
		return nil, err
	}
	return s, nil
}

func (s *Server) start(ctx context.Context) error {
	s.bin = os.Getenv("HP_APP_BIN")
	if s.bin == "" {
		app := os.Getenv("HP_APP_DIR")
		if app == "" {
			app = filepath.Join(repoRoot(), "..", "go-harbor-pilot")
		}
		s.bin = filepath.Join(s.dir, "harbor-pilot")
		build := exec.CommandContext(ctx, "go", "build", "-o", s.bin, "./cmd/harbor-pilot")
		build.Dir = app
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			return fmt.Errorf("build harbor-pilot in %s: %w\n%s", app, err, out)
		}
	}
	key, err := exec.CommandContext(ctx, s.bin, "keys", "generate").Output()
	if err != nil {
		return fmt.Errorf("generate master key: %w", err)
	}
	keyFile := filepath.Join(s.dir, "master.key")
	if err := os.WriteFile(keyFile, key, 0o600); err != nil {
		return err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	addr := l.Addr().String()
	_ = l.Close()
	s.URL = "http://" + addr
	s.env = append(os.Environ(), "HP_LISTEN="+addr, "HP_PUBLIC_URL="+s.URL, "HP_DATA_DIR="+filepath.Join(s.dir, "data"),
		"HP_MASTER_KEY_FILE="+keyFile, "HP_LOG_LEVEL=warn", "HP_MASTER_KEY=")
	if s.logFile, err = os.Create(filepath.Join(s.dir, "server.log")); err != nil {
		return err
	}
	s.cmd = exec.Command(s.bin, "serve") //nolint:gosec // test binary
	s.cmd.Env, s.cmd.Stdout, s.cmd.Stderr = s.env, s.logFile, s.logFile
	if err := s.cmd.Start(); err != nil {
		return err
	}
	if err := s.waitReady(ctx); err != nil {
		return err
	}
	if err := s.bootstrap(ctx); err != nil {
		return err
	}
	if s.Client, err = client.New(client.Options{BaseURL: s.URL, Token: s.Token, ManagedBy: "api"}); err != nil {
		return err
	}
	s.TF, err = client.New(client.Options{BaseURL: s.URL, Token: s.Token})
	return err
}

func (s *Server) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(s.URL + "/readyz") //nolint:noctx // loopback poll
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("server not ready; log:\n%s", s.Log())
}

var setupRE = regexp.MustCompile(`setup\?token=([A-Za-z0-9_\-]+)`)

func (s *Server) bootstrap(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, s.bin, "admin", "bootstrap") //nolint:gosec // test binary
	cmd.Env = s.env
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("admin bootstrap: %w", err)
	}
	m := setupRE.FindSubmatch(out)
	if m == nil {
		return errors.New("admin bootstrap printed no setup link")
	}
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	var sess struct {
		CSRF string `json:"csrf_token"`
	}
	call := func(method, path string, body, out any) error {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequestWithContext(ctx, method, s.URL+"/api/v1"+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", s.URL)
		if sess.CSRF != "" {
			req.Header.Set("X-CSRF-Token", sess.CSRF)
		}
		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, data)
		}
		return json.Unmarshal(data, out)
	}
	if err := call(http.MethodPost, "/auth/setup", map[string]string{"token": string(m[1]), "email": "admin@example.test",
		"display_name": "Admin", "password": "correct-horse-battery-staple"}, &sess); err != nil {
		return err
	}
	var sp struct {
		ID string `json:"id"`
	}
	if err := call(http.MethodPost, "/service-principals", map[string]any{"name": "terraform-acc", "permissions": Permissions}, &sp); err != nil {
		return err
	}
	var tok struct {
		Plaintext string `json:"plaintext"`
	}
	if err := call(http.MethodPost, "/service-principals/"+sp.ID+"/tokens", map[string]string{"name": "acc"}, &tok); err != nil {
		return err
	}
	s.Token = tok.Plaintext
	return nil
}

// Log returns the server log (for failure messages).
func (s *Server) Log() string {
	if s.logFile == nil {
		return ""
	}
	b, _ := os.ReadFile(s.logFile.Name())
	var keep []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		keep = append(keep, sc.Text())
	}
	if len(keep) > 50 {
		keep = keep[len(keep)-50:]
	}
	return strings.Join(keep, "\n")
}

// Stop terminates the server and removes its data.
func (s *Server) Stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = s.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = s.cmd.Process.Kill()
		}
	}
	if s.logFile != nil {
		_ = s.logFile.Close()
	}
	if os.Getenv("HP_ACC_KEEP") == "" {
		_ = os.RemoveAll(s.dir)
	}
}
