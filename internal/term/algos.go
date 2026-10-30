package term

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
)

// SSH algorithm options of a session. By default the library's modern
// algorithms are used. Old devices (a switch from 2010, an appliance whose
// firmware stopped at SSH-1.99) may only offer weak ones, such as
// diffie-hellman-group1-sha1 or aes128-cbc: "Allow legacy algorithms" adds
// them at the END of the lists, so modern ones are still preferred whenever
// the server offers them. The four lists take the OpenSSH syntax:
//
//	a,b,c      exactly these
//	+a,b       add to the end of the usual list
//	-a,b*      remove (a name may end in or start with * )
//	^a,b       put at the front of the usual list

// kinds of algorithm list
const (
	algoKex     = "key exchange"
	algoCipher  = "cipher"
	algoMAC     = "MAC"
	algoHostKey = "host key"
)

// algoSets is the usual list and everything known (weak ones included) for a kind.
func algoSets(kind string) (usual, known []string) {
	sup, bad := ssh.SupportedAlgorithms(), ssh.InsecureAlgorithms()
	switch kind {
	case algoKex:
		// the library's own default also offers diffie-hellman-group14-sha1
		usual = append(slices.Clone(sup.KeyExchanges), ssh.InsecureKeyExchangeDH14SHA1)
		return usual, append(slices.Clone(sup.KeyExchanges), bad.KeyExchanges...)
	case algoCipher:
		return sup.Ciphers, append(slices.Clone(sup.Ciphers), bad.Ciphers...)
	case algoMAC:
		return sup.MACs, append(slices.Clone(sup.MACs), bad.MACs...)
	default:
		return sup.HostKeys, append(slices.Clone(sup.HostKeys), bad.HostKeys...)
	}
}

func splitAlgos(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func dedupe(in []string) []string {
	var out []string
	for _, a := range in {
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}

// weakOf lists the weak algorithms of a kind that are not in the usual list.
func weakOf(kind string) []string {
	usual, known := algoSets(kind)
	var out []string
	for _, a := range known {
		if !slices.Contains(usual, a) {
			out = append(out, a)
		}
	}
	return out
}

// resolveAlgos returns the list to offer for one kind, or nil to keep the
// library's default (nothing was asked for).
func resolveAlgos(kind, spec string, legacy bool) ([]string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" && !legacy {
		return nil, nil
	}
	usual, known := algoSets(kind)
	list := slices.Clone(usual)
	if spec != "" {
		op := spec[0]
		items := splitAlgos(strings.TrimLeft(spec, "+-^"))
		for _, a := range items {
			if op == '-' || strings.ContainsAny(a, "*?") {
				continue // patterns are checked below
			}
			if !slices.Contains(known, a) {
				return nil, fmt.Errorf("%q is not a %s algorithm Termwright can use", a, kind)
			}
		}
		switch op {
		case '+':
			list = append(list, items...)
		case '^':
			list = append(slices.Clone(items), list...)
		case '-':
			list = slices.DeleteFunc(list, func(have string) bool {
				for _, pat := range items {
					if ok, _ := path.Match(pat, have); ok {
						return true
					}
				}
				return false
			})
		default:
			list = splitAlgos(spec)
		}
	}
	if legacy {
		list = append(list, weakOf(kind)...)
	}
	list = dedupe(list)
	if len(list) == 0 {
		return nil, fmt.Errorf("no %s algorithm is left in the list %q", kind, spec)
	}
	return list, nil
}

// applyAlgorithms sets the key exchange, cipher, MAC and host key lists of an
// SSH client config from the session's options. known is the host key
// algorithm list a known_hosts entry asks for (nil when there is none): it
// keeps priority, because the server's key must match what is recorded.
func applyAlgorithms(cfg *ssh.ClientConfig, s session.Session, known []string) error {
	var err error
	if cfg.KeyExchanges, err = resolveAlgos(algoKex, s.KexAlgorithms, s.LegacyAlgorithms); err != nil {
		return err
	}
	if cfg.Ciphers, err = resolveAlgos(algoCipher, s.Ciphers, s.LegacyAlgorithms); err != nil {
		return err
	}
	if cfg.MACs, err = resolveAlgos(algoMAC, s.MACs, s.LegacyAlgorithms); err != nil {
		return err
	}
	hk, err := resolveAlgos(algoHostKey, s.HostKeyAlgorithms, s.LegacyAlgorithms)
	if err != nil {
		return err
	}
	switch {
	case len(known) > 0 && hk != nil:
		// the recorded key's algorithms, then the rest of what was asked for
		cfg.HostKeyAlgorithms = dedupe(append(slices.Clone(known), hk...))
	case len(known) > 0:
		cfg.HostKeyAlgorithms = known
	default:
		cfg.HostKeyAlgorithms = hk
	}
	return nil
}

// legacyHint adds what to do to a handshake error that says the server and
// Termwright share no algorithm, naming what the server offers.
func legacyHint(err error, s session.Session) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, "no common algorithm") {
		return err
	}
	if s.LegacyAlgorithms {
		return fmt.Errorf("%w\nThe server offers none of the algorithms Termwright supports, not even the legacy ones (Session Options → Authentication → Algorithms).", err)
	}
	return fmt.Errorf("%w\nThis server only offers old algorithms that Termwright does not use by default. If you trust the network, tick \"Allow legacy algorithms\" in Session Options → Authentication → Algorithms for this session.", err)
}
