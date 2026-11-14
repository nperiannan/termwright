package main

import (
	"errors"
	"strings"
	"testing"
)

func TestWebThroughError(t *testing.T) {
	for _, c := range []struct{ err, want string }{
		{"ssh: rejected: administratively prohibited (open failed)", "TCP forwarding off"},
		{"ssh: rejected: connect failed (unknown host)", "cannot find www.example.com"},
		{"no answer from www.example.com:443 within 15s", "can it reach the internet"},
		{"something else", "could not open a connection"},
	} {
		if err := webThroughError("5520-24t-1", "www.example.com", errors.New(c.err)); !strings.Contains(err.Error(), c.want) || !strings.HasPrefix(err.Error(), "5520-24t-1 ") {
			t.Errorf("%s: %v", c.err, err)
		}
	}
}
