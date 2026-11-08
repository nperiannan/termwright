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

// Inventory fetches the team inventory; with wait > 0 the server answers when
// it is newer than rev, or after wait seconds.
func (c *Client) Inventory(ctx context.Context, rev int64, wait int) (Inventory, error) {
	var v Inventory
	cl := c
	if wait > 0 { // longer than the usual timeout
		cp := *c
		hc := *c.hc
		hc.Timeout = time.Duration(wait+20) * time.Second
		cp.hc = &hc
		cl = &cp
	}
	err := cl.call(ctx, http.MethodGet, fmt.Sprintf("/api/inventory?rev=%d&wait=%d", rev, wait), nil, &v)
	return v, err
}

// Check tells for each device whether the team has it already.
func (c *Client) Check(ctx context.Context, d []Device) ([]PushResult, error) {
	var v []PushResult
	err := c.call(ctx, http.MethodPost, "/api/inventory/check", devicesBody{Devices: d}, &v)
	return v, err
}

// Push offers devices to the team (added at once for owners and co-owners).
func (c *Client) Push(ctx context.Context, d []Device) ([]PushResult, error) {
	var v []PushResult
	err := c.call(ctx, http.MethodPost, "/api/inventory/push", devicesBody{Devices: d}, &v)
	return v, err
}

// AdminLink is a one-time sign-in path for the admin page ("/admin#c=…").
func (c *Client) AdminLink(ctx context.Context) (string, error) {
	var v AdminLink
	err := c.call(ctx, http.MethodPost, "/api/admin/link", struct{}{}, &v)
	return v.Path, err
}

// SaveDevices adds or changes inventory devices (owners and co-owners).
func (c *Client) SaveDevices(ctx context.Context, d []Device) error {
	return c.call(ctx, http.MethodPost, "/api/admin/devices", devicesBody{Devices: d}, nil)
}

// Presence tells the server which of the team's devices this member has open now.
func (c *Client) Presence(ctx context.Context, d []DeviceRef) error {
	if d == nil {
		d = []DeviceRef{}
	}
	return c.call(ctx, http.MethodPost, "/api/presence", map[string]any{"devices": d}, nil)
}

// Status is who uses and reserved which devices; with wait > 0 the server
// answers when it is newer than rev (or a notice waits), or after wait seconds.
func (c *Client) Status(ctx context.Context, rev int64, wait int) (Status, error) {
	return c.StatusSince(ctx, rev, -1, wait)
}

// StatusSince is Status for a client that has Last of lastRev already: Last
// is then left out unless it changed (lastRev -1: always included).
func (c *Client) StatusSince(ctx context.Context, rev, lastRev int64, wait int) (Status, error) {
	var v Status
	cl := c
	if wait > 0 {
		cp := *c
		hc := *c.hc
		hc.Timeout = time.Duration(wait+20) * time.Second
		cp.hc = &hc
		cl = &cp
	}
	q := fmt.Sprintf("/api/status?rev=%d&wait=%d", rev, wait)
	if lastRev >= 0 {
		q += fmt.Sprintf("&last=%d", lastRev)
	}
	err := cl.call(ctx, http.MethodGet, q, nil, &v)
	return v, err
}

// Reserve keeps a device until a time, with a note.
func (c *Client) Reserve(ctx context.Context, d DeviceRef, until time.Time, note string) (Reservation, error) {
	var v Reservation
	err := c.call(ctx, http.MethodPost, "/api/reserve", map[string]any{"device": d, "until": until.UTC().Format(time.RFC3339), "note": note}, &v)
	return v, err
}

// Release ends a reservation.
func (c *Client) Release(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodPost, "/api/release", MemberRef{ID: id}, nil)
}

// Connected tells the member who reserved a device that this member connected to it.
func (c *Client) Connected(ctx context.Context, d DeviceRef) error {
	return c.call(ctx, http.MethodPost, "/api/connected", map[string]any{"device": d}, nil)
}

// Watch starts (on) or stops waiting for a device to be free; free tells that it is free already.
func (c *Client) Watch(ctx context.Context, d DeviceRef, on bool) (free bool, err error) {
	var r struct {
		Free bool `json:"free"`
	}
	err = c.call(ctx, http.MethodPost, "/api/watch", map[string]any{"device": d, "on": on}, &r)
	return r.Free, err
}

// Backup downloads the team's backup (owner only).
func (c *Client) Backup(ctx context.Context) ([]byte, error) {
	return c.raw(ctx, http.MethodGet, "/api/admin/backup", nil)
}

// Restore puts a backup back (owner only).
func (c *Client) Restore(ctx context.Context, b []byte) error {
	_, err := c.raw(ctx, http.MethodPost, "/api/admin/restore", b)
	return err
}

func (c *Client) raw(ctx context.Context, method, path string, in []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, "https://"+c.Address+path, bytes.NewReader(in))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the team server %s cannot be reached: %w", c.Address, unwrapNet(err))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		return nil, &APIError{Status: resp.StatusCode, Msg: e.Error}
	}
	return b, nil
}

// SSOBegin starts a single sign-on (join, or again with s.Member): the page
// to open in the browser, and the request and secret to collect it with.
func (c *Client) SSOBegin(ctx context.Context, s SSOStart) (url, req, secret string, err error) {
	var v struct {
		URL    string `json:"url"`
		Req    string `json:"req"`
		Secret string `json:"secret"`
	}
	err = c.call(ctx, http.MethodPost, "/api/sso/start", s, &v)
	return v.URL, v.Req, v.Secret, err
}

// SSOCollect waits (up to about 50 s) for the browser's sign-in; waiting is
// true when it has not finished yet.
func (c *Client) SSOCollect(ctx context.Context, req, secret string) (j Joined, waiting bool, err error) {
	cp := *c
	hc := *c.hc
	hc.Timeout = 70 * time.Second
	cp.hc = &hc
	err = cp.call(ctx, http.MethodPost, "/api/sso/claim", map[string]string{"req": req, "secret": secret}, &j)
	var a *APIError
	if errors.As(err, &a) && a.Status == http.StatusAccepted {
		return j, true, nil
	}
	return j, false, err
}
