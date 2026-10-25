package term

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/crypto"
	"github.com/jcmturner/gokrb5/v8/gssapi"
	"github.com/jcmturner/gokrb5/v8/iana/flags"
	"github.com/jcmturner/gokrb5/v8/iana/keyusage"
	"github.com/jcmturner/gokrb5/v8/messages"
	"github.com/jcmturner/gokrb5/v8/spnego"
	"github.com/jcmturner/gokrb5/v8/types"
	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
)

// Kerberos (GSSAPI "gssapi-with-mic", RFC 4462) login in pure Go.
//
// Credentials come from, in order:
//  1. a file credential cache from kinit (KRB5CCNAME=FILE:… or /tmp/krb5cc_<uid>);
//  2. a Kerberos login made by Termwright itself: principal (session setting,
//     or user@REALM) and its password, asked once and kept for the app's
//     lifetime (tickets are cached per principal).
//
// The realm configuration is read from KRB5_CONFIG, /etc/krb5.conf,
// /Library/Preferences/edu.mit.Kerberos (macOS) or krb5.ini (Windows); without
// one, the KDCs are found through DNS SRV records of the realm.

// kerberosClients caches logged-in clients per principal.
var kerberosClients sync.Map // principal → *client.Client

func krb5ConfigPaths() []string {
	var p []string
	if v := os.Getenv("KRB5_CONFIG"); v != "" {
		p = append(p, strings.Split(v, string(os.PathListSeparator))...)
	}
	switch runtime.GOOS {
	case "windows":
		p = append(p, filepath.Join(os.Getenv("ProgramData"), "MIT", "Kerberos5", "krb5.ini"),
			filepath.Join(os.Getenv("WINDIR"), "krb5.ini"))
	case "darwin":
		p = append(p, "/etc/krb5.conf", "/Library/Preferences/edu.mit.Kerberos")
	default:
		p = append(p, "/etc/krb5.conf")
	}
	return p
}

// loadKrb5Config reads the first krb5.conf found, or builds a DNS-based one for realm.
func loadKrb5Config(realm string) (*config.Config, error) {
	for _, p := range krb5ConfigPaths() {
		if _, err := os.Stat(p); err == nil {
			cfg, err := config.Load(p)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", p, err)
			}
			if realm != "" && cfg.LibDefaults.DefaultRealm == "" {
				cfg.LibDefaults.DefaultRealm = realm
			}
			return cfg, nil
		}
	}
	if realm == "" {
		return nil, errors.New("no krb5.conf found - enter the principal as user@REALM so the KDC can be found in DNS")
	}
	return config.NewFromString(fmt.Sprintf("[libdefaults]\n default_realm = %s\n dns_lookup_kdc = true\n dns_lookup_realm = true\n udp_preference_limit = 1\n", realm))
}

// defaultCCachePath is where kinit leaves tickets.
func defaultCCachePath() string {
	if v := os.Getenv("KRB5CCNAME"); v != "" {
		if rest, ok := strings.CutPrefix(v, "FILE:"); ok {
			return rest
		}
		if !strings.Contains(v, ":") || (runtime.GOOS == "windows" && len(v) > 2 && v[1] == ':') {
			return v
		}
		return "" // API:, KCM:, KEYRING: caches are not files
	}
	if u, err := user.Current(); err == nil && runtime.GOOS != "windows" {
		return "/tmp/krb5cc_" + u.Uid
	}
	return ""
}

// splitPrincipal returns user and realm of "user@REALM".
func splitPrincipal(p string) (string, string) {
	if i := strings.LastIndex(p, "@"); i > 0 {
		return p[:i], strings.ToUpper(p[i+1:])
	}
	return p, ""
}

// kerberosClient returns a logged-in client for the session.
func (m *Manager) kerberosClient(ctx context.Context, t *Tab, s session.Session, loginUser string) (*client.Client, error) {
	principal := strings.TrimSpace(s.KerberosPrincipal)
	// 1. kinit's file cache
	if path := defaultCCachePath(); path != "" {
		if cc, err := credentials.LoadCCache(path); err == nil {
			name := cc.DefaultPrincipal.PrincipalName.PrincipalNameString() + "@" + cc.DefaultPrincipal.Realm
			if principal == "" || strings.EqualFold(principal, name) {
				if cfg, err := loadKrb5Config(cc.DefaultPrincipal.Realm); err == nil {
					if cl, err := client.NewFromCCache(cc, cfg, client.DisablePAFXFAST(true)); err == nil {
						return cl, nil
					}
				}
			}
		}
	}
	// 2. our own login
	if principal == "" {
		principal = loginUser
	}
	if v, ok := kerberosClients.Load(strings.ToLower(principal)); ok {
		return v.(*client.Client), nil
	}
	name, realm := splitPrincipal(principal)
	cfg, err := loadKrb5Config(realm)
	if err != nil {
		return nil, err
	}
	if realm == "" {
		realm = cfg.LibDefaults.DefaultRealm
	}
	if realm == "" {
		return nil, errors.New("Kerberos realm unknown: set the principal as user@REALM (Session Options → SSH2 Authentication)")
	}
	r, err := m.prompts.Ask(ctx, Prompt{TabID: t.id, Kind: "credentials", Title: "Kerberos Login",
		Message: "No Kerberos ticket found (kinit). Password for " + name + "@" + realm,
		Fields:  []PromptField{{Label: "Password", Secret: true}}})
	if err != nil {
		return nil, err
	}
	cl := client.NewWithPassword(name, realm, r.Values[0], cfg, client.DisablePAFXFAST(true))
	if err := cl.Login(); err != nil {
		return nil, fmt.Errorf("Kerberos login for %s@%s: %w", name, realm, err)
	}
	kerberosClients.Store(strings.ToLower(principal), cl)
	return cl, nil
}

// gssClient implements ssh.GSSAPIClient with a Kerberos client.
type gssClient struct {
	get func() (*client.Client, error)
	spn string              // overrides the "host@<server>" target when set
	key types.EncryptionKey // ticket session key, then the acceptor subkey from the AP-REP
	// acceptorKey: key is the acceptor's subkey (sets the MIC token flag)
	acceptorKey bool
}

// spnFromTarget turns GSSAPI's "service@host" into Kerberos' "service/host".
func spnFromTarget(target string) string {
	if svc, host, ok := strings.Cut(target, "@"); ok {
		return svc + "/" + host
	}
	return "host/" + target
}

func (g *gssClient) InitSecContext(target string, token []byte, _ bool) ([]byte, bool, error) {
	if token != nil {
		// the server's AP-REP (mutual authentication): it may carry the acceptor
		// subkey that protects the MIC
		var rep spnego.KRB5Token
		if err := rep.Unmarshal(token); err != nil {
			return nil, false, fmt.Errorf("Kerberos reply: %w", err)
		}
		if rep.IsKRBError() {
			return nil, false, fmt.Errorf("Kerberos error from the server: %s", rep.KRBError.Error())
		}
		if rep.IsAPRep() {
			b, err := crypto.DecryptEncPart(rep.APRep.EncPart, g.key, keyusage.AP_REP_ENCPART)
			if err != nil {
				return nil, false, fmt.Errorf("Kerberos reply: %w", err)
			}
			var part messages.EncAPRepPart
			if err := part.Unmarshal(b); err != nil {
				return nil, false, err
			}
			if len(part.Subkey.KeyValue) > 0 {
				g.key, g.acceptorKey = part.Subkey, true
			}
		}
		return nil, false, nil
	}
	cl, err := g.get()
	if err != nil {
		return nil, false, err
	}
	spn := spnFromTarget(target)
	if g.spn != "" {
		spn = spnFromTarget(g.spn)
	}
	tkt, key, err := cl.GetServiceTicket(spn)
	if err != nil {
		return nil, false, fmt.Errorf("Kerberos ticket for %s: %w", spn, err)
	}
	g.key = key
	// OpenSSH only accepts the user when mutual authentication and integrity are both requested
	tok, err := spnego.NewKRB5TokenAPREQ(cl, tkt, key,
		[]int{gssapi.ContextFlagMutual, gssapi.ContextFlagInteg, gssapi.ContextFlagConf}, []int{flags.APOptionMutualRequired})
	if err != nil {
		return nil, false, err
	}
	out, err := tok.Marshal()
	return out, true, err
}

func (g *gssClient) GetMIC(micField []byte) ([]byte, error) {
	mic := gssapi.MICToken{Payload: micField}
	if g.acceptorKey {
		mic.Flags = gssapi.MICTokenFlagAcceptorSubkey
	}
	if err := mic.SetChecksum(g.key, keyusage.GSSAPI_INITIATOR_SIGN); err != nil {
		return nil, err
	}
	return mic.Marshal()
}

func (g *gssClient) DeleteSecContext() error { return nil }

// kerberosAuth returns the "gssapi-with-mic" auth method for a session.
func (m *Manager) kerberosAuth(ctx context.Context, t *Tab, s session.Session, user, host string) ssh.AuthMethod {
	var once sync.Once
	var cl *client.Client
	var err error
	g := &gssClient{spn: strings.ReplaceAll(strings.TrimSpace(s.KerberosSPN), "%H", host), get: func() (*client.Client, error) {
		once.Do(func() { cl, err = m.kerberosClient(ctx, t, s, user) })
		return cl, err
	}}
	return ssh.GSSAPIWithMICAuthMethod(g, host)
}
