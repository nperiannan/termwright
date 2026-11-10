package sim

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// Generate fills a format with random values: {N:k} k digits, {A:k} k
// letters, {H:k} / {h:k} k hex digits, {YY} a year 21-26, {WW} a week 01-52,
// {L:abc} one of the letters, {R:a-b} a number from a to b (as wide as b).
// Everything else is kept. "AB{R:01-14}{YY}{WW}Q-4{N:4}" → AB032611Q-40036.
func Generate(f string) string {
	return genToken.ReplaceAllStringFunc(f, func(t string) string {
		m := genToken.FindStringSubmatch(t)
		k, arg := m[1], m[2]
		n, _ := strconv.Atoi(arg)
		if n <= 0 {
			n = 1
		}
		var b strings.Builder
		switch k {
		case "N":
			for i := 0; i < n; i++ {
				b.WriteByte(byte('0' + rnd(10)))
			}
		case "A":
			for i := 0; i < n; i++ {
				b.WriteByte(letters[rnd(len(letters))])
			}
		case "H", "h":
			for i := 0; i < n; i++ {
				c := "0123456789ABCDEF"[rnd(16)]
				if k == "h" {
					c = strings.ToLower(string(c))[0]
				}
				b.WriteByte(c)
			}
		case "YY":
			return fmt.Sprintf("%02d", 21+rnd(6))
		case "WW":
			return fmt.Sprintf("%02d", 1+rnd(52))
		case "L":
			if arg == "" {
				return ""
			}
			return string(arg[rnd(len(arg))])
		case "R":
			lo, hi, _ := strings.Cut(arg, "-")
			a, _ := strconv.Atoi(lo)
			z, _ := strconv.Atoi(hi)
			if z < a {
				z = a
			}
			return fmt.Sprintf("%0*d", len(hi), a+rnd(z-a+1))
		}
		return b.String()
	})
}

var genToken = regexp.MustCompile(`\{(N|A|H|h|YY|WW|L|R)(?::([^}]*))?\}`)

const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ"

func rnd(n int) int {
	if n <= 1 {
		return 0
	}
	v, _ := rand.Int(rand.Reader, big.NewInt(int64(n)))
	return int(v.Int64())
}

func randID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// newerThan: version a is at least b ("33.7.1.6" ≥ "33.6").
func newerThan(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			fmt.Sscan(pa[i], &x)
		}
		fmt.Sscan(pb[i], &y)
		if x != y {
			return x > y
		}
	}
	return true
}
