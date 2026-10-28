package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDownloads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, `[{"assets":[{"download_count":3},{"download_count":4}]},{"assets":[]},{"assets":[{"download_count":5}]}]`)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	old := API
	API = srv.URL
	defer func() { API = old }()
	n, err := Downloads(context.Background(), "o/r", "")
	if err != nil || n != 12 {
		t.Fatalf("%d %v", n, err)
	}
	if _, err := Downloads(context.Background(), "o/missing", ""); err == nil {
		t.Fatal("expected an error for a missing repository")
	}
}
