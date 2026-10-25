package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"0.6.10", "0.6.9", 1}, {"v0.7.0", "0.6.0-dev", 1}, {"0.6.0", "0.6.0~git9d57820", 0}, {"0.5.9", "0.6.0", -1}} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q)=%d", c.a, c.b, got)
		}
	}
}

func TestPickAsset(t *testing.T) {
	names := []string{"termwright-macos-arm64.zip", "Termwright-0.7.0-macos-arm64.dmg", "Termwright-0.7.0-windows-amd64-setup.exe",
		"termwright-windows-amd64.zip", "termwright_0.7.0_amd64.deb", "termwright_0.7.0_arm64.deb", "termwright-linux-arm64.tar.gz"}
	for _, c := range []struct {
		os, arch, want string
	}{{"darwin", "arm64", "Termwright-0.7.0-macos-arm64.dmg"}, {"windows", "amd64", "Termwright-0.7.0-windows-amd64-setup.exe"},
		{"linux", "arm64", "termwright_0.7.0_arm64.deb"}, {"linux", "amd64", "termwright_0.7.0_amd64.deb"}} {
		if i := PickAsset(names, c.os, c.arch); i < 0 || names[i] != c.want {
			t.Errorf("%s/%s -> %d", c.os, c.arch, i)
		}
	}
}

func TestCheckAndDownload(t *testing.T) {
	var srv *httptest.Server
	asset := map[string]string{"darwin": "Termwright-0.7.0-macos-arm64.dmg", "windows": "Termwright-0.7.0-windows-amd64-setup.exe"}[runtime.GOOS]
	if asset == "" {
		asset = "termwright_0.7.0_" + runtime.GOARCH + ".deb"
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(404)
			return
		}
		switch r.URL.Path {
		case "/repos/o/r/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.7.0","name":"Termwright v0.7.0","body":"notes","html_url":"h",
				"assets":[{"name":%q,"url":"%s/repos/o/r/releases/assets/1","size":5}]}`, asset, srv.URL)
		case "/repos/o/r/releases/tags/nightly":
			fmt.Fprint(w, `{"tag_name":"nightly","name":"Termwright nightly (abcdef1)","assets":[]}`)
		case "/repos/o/r/releases/assets/1":
			if r.Header.Get("Accept") != "application/octet-stream" {
				w.WriteHeader(415)
				return
			}
			w.Write([]byte("hello"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	API = srv.URL
	ctx := context.Background()
	if _, err := Check(ctx, "o/r", "stable", "", "0.6.0"); err == nil {
		t.Fatal("expected error without token")
	}
	info, err := Check(ctx, "o/r", "stable", "tok", "0.6.0")
	if err != nil || !info.Newer || info.Latest != "0.7.0" || info.AssetName != asset {
		t.Fatalf("%+v %v", info, err)
	}
	if info, _ := Check(ctx, "o/r", "stable", "tok", "0.7.0"); info.Newer {
		t.Fatal("same version reported newer")
	}
	if n, _ := Check(ctx, "o/r", "nightly", "tok", "0.6.0~gitabcdef1"); n.Newer {
		t.Fatal("same nightly reported newer")
	}
	if n, _ := Check(ctx, "o/r", "nightly", "tok", "0.6.0~git1234567"); !n.Newer {
		t.Fatal("different nightly not reported")
	}
	dest := filepath.Join(t.TempDir(), asset)
	if err := Download(ctx, info.AssetURL, "tok", dest, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "hello" {
		t.Fatalf("got %q", b)
	}
	if err := Download(ctx, "https://evil.example/x", "tok", dest, nil); err == nil {
		t.Fatal("foreign URL accepted")
	}
}

// A public repository works without a token, and a stale token (expired, or
// limited to other repositories) does not break the check or the download.
func TestPublicRepoWithStaleToken(t *testing.T) {
	var srv *httptest.Server
	asset := map[string]string{"darwin": "Termwright-0.11.0-macos-arm64.dmg", "windows": "Termwright-0.11.0-windows-amd64-setup.exe"}[runtime.GOOS]
	if asset == "" {
		asset = "termwright_0.11.0_" + runtime.GOARCH + ".deb"
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(401) // the old token is no longer valid
			return
		}
		switch r.URL.Path {
		case "/repos/o/r/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.11.0","name":"Termwright v0.11.0","assets":[{"name":%q,"url":"%s/repos/o/r/releases/assets/7","size":2}]}`, asset, srv.URL)
		case "/repos/o/r/releases/assets/7":
			w.Write([]byte("ok"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	API = srv.URL
	ctx := context.Background()
	for _, tok := range []string{"", "stale"} {
		info, err := Check(ctx, "o/r", "stable", tok, "0.10.6")
		if err != nil || !info.Newer || info.AssetName != asset {
			t.Fatalf("token %q: %+v %v", tok, info, err)
		}
		dest := filepath.Join(t.TempDir(), asset)
		if err := Download(ctx, info.AssetURL, tok, dest, nil); err != nil {
			t.Fatalf("download with token %q: %v", tok, err)
		}
	}
	if _, err := Check(ctx, "o/r", "nightly", "", "0.10.6"); err == nil || !strings.Contains(err.Error(), "Stable") {
		t.Fatalf("missing nightly should suggest Stable: %v", err)
	}
}
