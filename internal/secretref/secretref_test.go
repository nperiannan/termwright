package secretref

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeCLI(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestResolve(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	// 1Password: op read --no-newline op://Network/core-sw1/password
	fakeCLI(t, bin, "op", `[ "$1 $2 $3" = "read --no-newline op://Network/core-sw1/password" ] && printf 'op-secret' && exit 0; echo "bad args $*" >&2; exit 1`)
	// Bitwarden: locked until unlocked with the master password "mpw"
	fakeCLI(t, bin, "bw", `case "$1" in
unlock) [ "$TW_BW_PASSWORD" = "mpw" ] && { printf 'SESSIONKEY'; exit 0; }; echo "Invalid master password." >&2; exit 1;;
get) [ "$BW_SESSION" = "SESSIONKEY" ] || { echo "Vault is locked." >&2; exit 1; }; [ "$2" = "password" ] && echo "bw-secret" && exit 0; [ "$2" = "username" ] && echo "bw-user" && exit 0;;
esac; exit 1`)
	// HashiCorp Vault: KV v2 at secret/, KV v1 at kv1/
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "tok" {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/v1/secret/data/net/sw1":
			w.Write([]byte(`{"data":{"data":{"password":"vault-v2","user":"admin"}}}`))
		case "/v1/kv1/net/sw2":
			w.Write([]byte(`{"data":{"password":"vault-v1"}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "tok")

	r := &Resolver{}
	asked := 0
	ask := func(title, msg string) (string, error) { asked++; return "mpw", nil }
	ctx := context.Background()
	for ref, want := range map[string]string{
		"op://Network/core-sw1/password": "op-secret",
		"bw://core-sw1":                  "bw-secret",
		"bw://core-sw1#username":         "bw-user",
		"vault://secret/net/sw1":         "vault-v2",
		"vault://secret/net/sw1#user":    "admin",
		"vault://kv1/net/sw2":            "vault-v1",
		"cmd://echo from-command":        "from-command",
	} {
		got, err := r.Resolve(ctx, ref, ask)
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", ref, got, err, want)
		}
	}
	if asked != 1 {
		t.Errorf("Bitwarden master password asked %d times, want 1", asked)
	}
	if _, err := r.Resolve(ctx, "vault://secret/missing", nil); err == nil {
		t.Error("missing Vault secret should fail")
	}
	if _, err := r.Resolve(ctx, "cmd://exit 3", nil); err == nil {
		t.Error("failing command should fail")
	}
	if !IsRef("op://a/b/c") || IsRef("hunter2") {
		t.Error("IsRef")
	}
	// cached: changing the CLI does not matter until Forget
	fakeCLI(t, bin, "op", `printf changed`)
	if v, _ := r.Resolve(ctx, "op://Network/core-sw1/password", nil); v != "op-secret" {
		t.Errorf("cache: %q", v)
	}
	r.Forget()
	if v, _ := r.Resolve(ctx, "op://Network/core-sw1/password", nil); !strings.Contains(v, "changed") {
		t.Errorf("after Forget: %q", v)
	}
}
