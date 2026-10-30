package session

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/pbkdf2"

	"github.com/termwright/termwright/internal/sessionfile"
)

// mRemoteNG keeps its connections in confCons.xml:
//
//	<mrng:Connections xmlns:mrng="http://mremoteng.org" EncryptionEngine="AES" BlockCipherMode="GCM"
//	    KdfIterations="1000" FullFileEncryption="false" Protected="…" ConfVersion="2.6">
//	  <Node Name="Lab" Type="Container" Username="admin" Protocol="SSH2" Port="22" …>
//	    <Node Name="sw1" Type="Connection" Hostname="10.0.0.1" Protocol="SSH2" Port="22"
//	          Username="admin" Password="<encrypted>" InheritUsername="False" … />
//	  </Node>
//	</mrng:Connections>
//
// A connection can inherit its user name, password, domain, protocol and port
// from the container it is in (the Inherit… attributes).

type mrngNode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Nodes   []mrngNode `xml:"Node"`
	Text    string     `xml:",chardata"`
}

func (n mrngNode) attr(name string) string {
	for _, a := range n.Attrs {
		if strings.EqualFold(a.Name.Local, name) {
			return a.Value
		}
	}
	return ""
}

func (n mrngNode) flag(name string) bool {
	return strings.EqualFold(strings.TrimSpace(n.attr(name)), "true")
}

// isMRemoteNG reports whether data is an mRemoteNG connections file.
func isMRemoteNG(data []byte) bool {
	head := data
	if len(head) > 4096 {
		head = head[:4096]
	}
	return bytes.Contains(head, []byte("mremoteng.org")) || bytes.Contains(head, []byte(":Connections")) && bytes.Contains(head, []byte("ConfVersion"))
}

// MRemoteNGSession is a parsed connection with its password ("" = none).
type MRemoteNGSession struct {
	Session  Session
	Password string
}

// mrngValues are the settings a container passes down to what is inside it.
type mrngValues struct {
	user, domain, password, protocol, port string
}

// mrngCrypto decrypts the passwords of a file.
type mrngCrypto struct {
	gcm        bool
	iterations int
	master     string
}

// ParseMRemoteNG reads a confCons.xml. Connections of protocols Termwright does
// not have (HTTP, ICA, Rlogin, SSH1…) are listed in skipped. master is the file's
// own master password ("" = mRemoteNG's default, which is what a file without
// one uses). Passwords that cannot be decrypted are counted in locked.
func ParseMRemoteNG(data []byte, master string) (list []MRemoteNGSession, skipped []string, locked int, err error) {
	var root mrngNode
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, nil, 0, fmt.Errorf("mRemoteNG connections: %w", err)
	}
	if len(root.Nodes) == 0 && strings.EqualFold(strings.TrimSpace(root.attr("FullFileEncryption")), "true") {
		return nil, nil, 0, fmt.Errorf("this mRemoteNG file is encrypted as a whole: in mRemoteNG turn off Tools → Options → Security → Encrypt complete connection file, save, and import that file")
	}
	c := newMRNGCrypto(root, master)
	var walk func(nodes []mrngNode, folder string, parent mrngValues)
	walk = func(nodes []mrngNode, folder string, parent mrngValues) {
		for _, n := range nodes {
			name := strings.TrimSpace(n.attr("Name"))
			own := mrngValues{
				user: n.attr("Username"), domain: n.attr("Domain"), password: n.attr("Password"),
				protocol: n.attr("Protocol"), port: n.attr("Port"),
			}
			eff := own
			if n.flag("InheritUsername") {
				eff.user = parent.user
			}
			if n.flag("InheritDomain") {
				eff.domain = parent.domain
			}
			if n.flag("InheritPassword") {
				eff.password = parent.password
			}
			if n.flag("InheritProtocol") {
				eff.protocol = parent.protocol
			}
			if n.flag("InheritPort") {
				eff.port = parent.port
			}
			if strings.EqualFold(n.attr("Type"), "Container") {
				sub := cleanFolder(folder + "/" + safeName(name))
				walk(n.Nodes, sub, eff)
				continue
			}
			if name == "" {
				name = strings.TrimSpace(n.attr("Hostname"))
			}
			host := strings.TrimSpace(n.attr("Hostname"))
			proto, supported := mrngProtocol(eff.protocol)
			label := JoinPath(folder, name)
			if !supported {
				skipped = append(skipped, label+" (mRemoteNG protocol "+strings.TrimSpace(eff.protocol)+" is not supported)")
				continue
			}
			s := Defaults()
			s.Name, s.Folder, s.Hostname, s.Protocol = safeName(name), folder, host, proto
			s.Username = strings.TrimSpace(eff.user)
			if p, err := strconv.Atoi(strings.TrimSpace(eff.port)); err == nil && p > 0 && p < 65536 {
				s.Port = p
			} else {
				s.Port = defaultPortFor(proto)
				switch proto {
				case ProtoRDP:
					s.Port = 3389
				case ProtoVNC:
					s.Port = 5900
				}
			}
			if proto == ProtoRDP {
				s.RDPDomain = strings.TrimSpace(eff.domain)
			}
			if d := strings.TrimSpace(n.attr("Descr")); d != "" {
				s.Description = strings.Split(strings.ReplaceAll(d, "\r\n", "\n"), "\n")
			}
			pw := ""
			if eff.password != "" {
				if v, ok := c.decrypt(eff.password); ok {
					pw = v
				} else {
					locked++
				}
			}
			if pw != "" {
				s.SavePassword = true
			}
			list = append(list, MRemoteNGSession{Session: s, Password: pw})
		}
	}
	walk(root.Nodes, "", mrngValues{})
	return list, skipped, locked, nil
}

// safeName makes a session or folder name that is a valid file name.
func safeName(s string) string {
	s = strings.TrimSpace(strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(s))
	if s == "" || s == "." || s == ".." {
		return "session"
	}
	return s
}

func mrngProtocol(p string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(p)) {
	case "SSH2":
		return ProtoSSH2, true
	case "TELNET":
		return ProtoTelnet, true
	case "RAW":
		return ProtoRaw, true
	case "RDP":
		return ProtoRDP, true
	case "VNC":
		return ProtoVNC, true
	}
	return "", false
}

// ---- passwords ----

const mrngDefaultMaster = "mR3m"

func newMRNGCrypto(root mrngNode, master string) *mrngCrypto {
	c := &mrngCrypto{master: master, iterations: 1000}
	if c.master == "" {
		c.master = mrngDefaultMaster
	}
	if n, err := strconv.Atoi(strings.TrimSpace(root.attr("KdfIterations"))); err == nil && n > 0 {
		c.iterations = n
	}
	mode := strings.ToUpper(strings.TrimSpace(root.attr("BlockCipherMode")))
	c.gcm = mode != "CBC"
	return c
}

// decrypt returns the plain text of one stored value.
func (c *mrngCrypto) decrypt(b64 string) (string, bool) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(raw) < 16 {
		return "", false
	}
	var plain []byte
	if c.gcm {
		plain = mrngGCM(raw, c.master, c.iterations)
	}
	if plain == nil {
		plain = mrngCBC(raw, c.master)
	}
	if plain == nil || !utf8.Valid(plain) {
		return "", false
	}
	return string(plain), true
}

// mrngGCM opens salt(16) + nonce(16) + ciphertext + tag(16) with AES-256-GCM,
// the key from PBKDF2-HMAC-SHA1 of the master password and the salt. The salt
// is the authenticated data (other conventions are tried too).
func mrngGCM(raw []byte, master string, iterations int) []byte {
	if len(raw) < 16+16+16 {
		return nil
	}
	salt, nonce, ct := raw[:16], raw[16:32], raw[32:]
	key := pbkdf2.Key([]byte(master), salt, iterations, 32, sha1.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	g, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		return nil
	}
	for _, aad := range [][]byte{salt, nil, nonce} {
		if p, err := g.Open(nil, nonce, ct, aad); err == nil {
			return p
		}
	}
	return nil
}

// mrngCBC opens the older format: iv(16) + AES-CBC ciphertext, the key being
// the MD5 of the master password, PKCS#7 padding.
func mrngCBC(raw []byte, master string) []byte {
	if len(raw) < 32 || (len(raw)-16)%16 != 0 {
		return nil
	}
	sum := md5.Sum([]byte(master))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil
	}
	iv, ct := raw[:16], raw[16:]
	out := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ct)
	pad := int(out[len(out)-1])
	if pad < 1 || pad > 16 || pad > len(out) {
		return nil
	}
	for _, b := range out[len(out)-pad:] {
		if int(b) != pad {
			return nil
		}
	}
	return out[:len(out)-pad]
}

// importMRemoteNG imports a confCons.xml; passwords go to the OS keychain
// (through res.Passwords), never into the session files. opt.ConfigPassphrase is
// the file's own master password, when it has one.
func (s *Store) importMRemoteNG(data []byte, opt ImportOptions) (*ImportResult, error) {
	list, skipped, locked, err := ParseMRemoteNG(data, opt.ConfigPassphrase)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{Skipped: skipped}
	for _, it := range list {
		f := sessionfile.NewFile()
		it.Session.Apply(f)
		dest := s.importOne(JoinPath(it.Session.Folder, it.Session.Name), f, opt, res)
		if dest != "" && it.Password != "" {
			if res.Passwords == nil {
				res.Passwords = map[string]string{}
			}
			res.Passwords[dest] = it.Password
		}
	}
	if locked > 0 {
		res.PasswordErrors = append(res.PasswordErrors, fmt.Sprintf("%d saved passwords were not imported: they are protected by a master password of the file. Import again and type that master password in the passphrase box.", locked))
	}
	return res, nil
}
