package sim

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The demo pack (testdata/demo): a made-up OS that uses every part of the engine.
func TestDemoPack(t *testing.T) {
	p, err := LoadPackFile("testdata/demo")
	if err != nil {
		t.Fatal(err)
	}
	runTranscripts(t, p)
}

// A device pack from outside (its own tests): TW_PACK=path/to/pack.vm or folder.
func TestPackFromEnv(t *testing.T) {
	path := os.Getenv("TW_PACK")
	if path == "" {
		t.Skip("TW_PACK not set")
	}
	p, err := LoadPackFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Tests) == 0 {
		t.Fatal("the pack has no tests")
	}
	runTranscripts(t, p)
}

func runTranscripts(t *testing.T, p *Pack) {
	var names []string
	for n := range p.Tests {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t.Run(strings.TrimSuffix(filepath.Base(n), ".txt"), func(t *testing.T) {
			for _, f := range RunTranscript(p, p.Tests[n]) {
				t.Error(f)
			}
		})
	}
}
