package team

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ErrFingerprint: the server's certificate is not the one in the invite.
var ErrFingerprint = errors.New("the team server's certificate is not the one in the invite - it may be another server, or the server was set up again")

// Client talks to one team server, trusting only the certificate with the
// fingerprint it was given.
type Client struct {
	Address     string // host:port
	Fingerprint string
	Token       string
	hc          *http.Client
}

// NewClient makes a client; token may be "" before joining.
func NewClient(address, fingerprint, token string) *Client {
	fp := NormFingerprint(fingerprint)
	tr := &http.Transport{
		Proxy:               nil, // a lab server: never through a web proxy
		DialContext:         (&net.Dialer{Timeout: 8 * time.Second}).DialContext,
		TLSHandshakeTimeout: 8 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // self-signed: checked by fingerprint below
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return ErrFingerprint
				}
				sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
				if hex.EncodeToString(sum[:]) != fp {
					return ErrFingerprint
				}
				return nil
			},
		},
	}
	return &Client{Address: address, Fingerprint: fp, Token: token, hc: &http.Client{Transport: tr, Timeout: 15 * time.Second}}
}

// APIError is an error answer of the server.
type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string { return e.Msg }

// Unauthorized: the member was removed (or the token is wrong).
func Unauthorized(err error) bool {
	var a *APIError
	return errors.As(err, &a) && a.Status == http.StatusUnauthorized
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://"+c.Address+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		if errors.Is(err, ErrFingerprint) || strings.Contains(err.Error(), ErrFingerprint.Error()) {
			return ErrFingerprint
		}
		return fmt.Errorf("the team server %s cannot be reached: %w", c.Address, unwrapNet(err))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return &APIError{Status: resp.StatusCode, Msg: e.Error}
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func unwrapNet(err error) error {
	var op *net.OpError
	if errors.As(err, &op) {
		return op
	}
	return err
}

// Info asks the server about itself (no membership needed).
func (c *Client) Info(ctx context.Context) (Info, error) {
	var v Info
	err := c.call(ctx, http.MethodGet, "/api/info", nil, &v)
	return v, err
}

// Join joins with an invite key or the owner setup code.
func (c *Client) Join(ctx context.Context, r JoinRequest) (Joined, error) {
	var v Joined
	err := c.call(ctx, http.MethodPost, "/api/join", r, &v)
	return v, err
}

// Team is the team with its members.
func (c *Client) Team(ctx context.Context) (Team, error) {
	var v Team
	err := c.call(ctx, http.MethodGet, "/api/team", nil, &v)
	return v, err
}

// RotateInvite makes a new invite key; the old one stops working.
func (c *Client) RotateInvite(ctx context.Context) (string, error) {
	var v struct {
		Invite string `json:"invite"`
	}
	err := c.call(ctx, http.MethodPost, "/api/invite/rotate", struct{}{}, &v)
	return v.Invite, err
}

// SetRole makes a member co-owner or member.
func (c *Client) SetRole(ctx context.Context, id, role string) error {
	return c.call(ctx, http.MethodPost, "/api/members/role", RoleChange{ID: id, Role: role}, nil)
}

// Remove takes a member out of the team.
func (c *Client) Remove(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodPost, "/api/members/remove", MemberRef{ID: id}, nil)
}

// Leave leaves the team.
func (c *Client) Leave(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/api/leave", struct{}{}, nil)
}
