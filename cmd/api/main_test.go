package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func noEnv(string) (string, bool) { return "", false }

// writeKeyPair writes a fresh RSA key pair to dir and returns the paths.
func writeKeyPair(t *testing.T, dir string) (priv, pub string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	priv, pub = filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	if err := os.WriteFile(priv, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(pub, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	return priv, pub
}

// freePort returns a port that was free a moment ago. The configuration
// rejects port 0, and this test never connects to the server, so the small
// window in which another process could take the port is acceptable.
func freePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// TestRunServesUntilSIGTERM boots the real binary wiring on an ephemeral port
// and checks that SIGTERM triggers a clean shutdown with exit code 0.
func TestRunServesUntilSIGTERM(t *testing.T) {
	if testing.Short() {
		t.Skip("boots the full application")
	}
	dir := t.TempDir()
	priv, pub := writeKeyPair(t, dir)
	logFile := filepath.Join(dir, "app.log")
	env := map[string]string{
		"APP_ADDRESS":              "127.0.0.1",
		"APP_PORT":                 strconv.Itoa(freePort(t)),
		"APP_LOG_FILE_PATH":        logFile,
		"APP_METRICS_PATH":         "",
		"APP_JWT_PRIVATE_KEY_PATH": priv,
		"APP_JWT_PUBLIC_KEY_PATH":  pub,
	}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	done := make(chan int, 1)
	var stderr strings.Builder
	go func() { done <- run(nil, lookup, io.Discard, &stderr) }()

	// SIGTERM is only safe once run has installed its signal handler, which
	// happens before the server logs that it started.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if b, _ := os.ReadFile(logFile); strings.Contains(string(b), "HTTP server started") {
			break
		}
		select {
		case code := <-done:
			t.Fatalf("run() exited early with %d: %s", code, stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("run() = %d after SIGTERM, want 0 (stderr %q)", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run() did not return after SIGTERM")
	}
}

func TestRunExitCodes(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "help prints usage", args: []string{"-h"}, wantCode: 0, wantStdout: "APP_PORT"},
		{name: "unknown flag", args: []string{"-nope"}, wantCode: 1, wantStderr: "Configuration error"},
		{name: "missing key files", args: []string{"-jwt-public-key-path", "/does/not/exist.pem"}, wantCode: 1, wantStderr: "public key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			if code := run(tt.args, noEnv, &stdout, &stderr); code != tt.wantCode {
				t.Fatalf("run() = %d, want %d (stderr %q)", code, tt.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}
