// Package team lets the Termwrights of a team see which devices the others use.
// A team runs its own small server (Server, the termwright-team program, usually
// in a Docker container on a host of the team's lab); every member's Termwright
// talks to it with Client over HTTPS, checking the server's certificate by its
// fingerprint (it is self-signed: no certificate authority is needed).
//
// Only who is who and device status is shared - never passwords, keys or
// session files.
package team

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Roles of a member. The owner set the server up; co-owners may manage it too.
const (
	RoleOwner   = "owner"
	RoleCoOwner = "co-owner"
	RoleMember  = "member"
)

// DefaultPort is the port inside the container (and the first one tried on the host).
const DefaultPort = 7443

// Info is what a server tells anyone (GET /api/info): enough to show before joining.
type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Members int    `json:"members"`
	Online  int    `json:"online"`
	Owner   bool   `json:"owner"` // an owner has joined (the setup code is used)
}

// JoinRequest joins a team with the invite key, or as its owner with the setup code.
type JoinRequest struct {
	Invite    string `json:"invite,omitempty"`
	OwnerCode string `json:"ownerCode,omitempty"`
	Name      string `json:"name"`    // display name, e.g. "Natarajan"
	User      string `json:"user"`    // OS user
	Machine   string `json:"machine"` // computer name
}

// Joined is the answer to a join: the member's id and its token (kept in the
// keychain; it is the member's password for the server).
type Joined struct {
	MemberID string `json:"memberId"`
	Token    string `json:"token"`
	Role     string `json:"role"`
	Team     string `json:"team"`
}

// Member is one person of the team, as the others see it.
type Member struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	User     string `json:"user"`
	Machine  string `json:"machine"`
	Role     string `json:"role"`
	Joined   string `json:"joined"`   // RFC 3339
	LastSeen string `json:"lastSeen"` // RFC 3339
	Online   bool   `json:"online"`
}

// Team is the team as a member sees it (GET /api/team). Invite is filled in
// for owners and co-owners only.
type Team struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Me      string   `json:"me"`
	Role    string   `json:"role"`
	Invite  string   `json:"invite,omitempty"`
	Members []Member `json:"members"`
}

// RoleChange and MemberRef are the bodies of the member management calls.
type RoleChange struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}
type MemberRef struct {
	ID string `json:"id"`
}

// Invitation is what an invite link holds.
type Invitation struct {
	Address     string // host:port
	Key         string // the invite key
	Fingerprint string // SHA-256 of the server certificate, hex
	Name        string // team name, for showing only
}

// InviteScheme starts an invite link: termwright-team://host:port/?k=…&fp=…&n=…
const InviteScheme = "termwright-team://"

// String is the invite link.
func (iv Invitation) String() string {
	q := url.Values{}
	q.Set("k", iv.Key)
	q.Set("fp", iv.Fingerprint)
	if iv.Name != "" {
		q.Set("n", iv.Name)
	}
	return InviteScheme + iv.Address + "/?" + q.Encode()
}

// ParseInvite reads an invite link (spaces and line breaks from chat are ignored).
func ParseInvite(s string) (Invitation, error) {
	s = strings.Join(strings.Fields(s), "")
	if !strings.HasPrefix(strings.ToLower(s), InviteScheme) {
		return Invitation{}, errors.New("not a Termwright team invite (it starts with " + InviteScheme + ")")
	}
	u, err := url.Parse("https://" + s[len(InviteScheme):])
	if err != nil {
		return Invitation{}, errors.New("the invite is damaged: " + err.Error())
	}
	iv := Invitation{Address: u.Host, Key: u.Query().Get("k"), Fingerprint: NormFingerprint(u.Query().Get("fp")), Name: u.Query().Get("n")}
	host, port, err := net.SplitHostPort(iv.Address)
	if err != nil || host == "" {
		return Invitation{}, errors.New("the invite has no server address")
	}
	if p, err := strconv.Atoi(port); err != nil || p <= 0 || p > 65535 {
		return Invitation{}, errors.New("the invite has a wrong port")
	}
	if iv.Key == "" || len(iv.Fingerprint) != 64 {
		return Invitation{}, errors.New("the invite is incomplete - copy all of it")
	}
	return iv, nil
}

// NormFingerprint makes "AB:CD…" and "abcd…" the same.
func NormFingerprint(fp string) string {
	return strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(strings.TrimSpace(fp)))
}

func randToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}
