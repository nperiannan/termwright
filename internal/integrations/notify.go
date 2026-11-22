package integrations

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Notify says where run results are sent besides the desktop notification: a Teams (or Slack, or any
// incoming-webhook) channel and e-mail. The webhook address and the mail password are in the credential store.
type Notify struct {
	// Webhook: on (the address is the "webhook" token).
	Webhook bool `json:"webhook,omitempty"`
	// Mail server and addresses.
	SMTPHost string   `json:"smtpHost,omitempty"`
	SMTPPort int      `json:"smtpPort,omitempty"` // 587 (STARTTLS) by default; 465 = TLS
	SMTPUser string   `json:"smtpUser,omitempty"`
	From     string   `json:"from,omitempty"`
	To       []string `json:"to,omitempty"`
	// When: "failures" (default: failed or not run) or "always".
	When string `json:"when,omitempty"`
}

// Kinds of the notification secrets.
const (
	KindWebhook = "webhook"
	KindSMTP    = "smtp"
)

// Message is a run's result for people.
type Message struct {
	Title  string
	Text   string   // a line or two
	Lines  []string // details (failed tests)
	Failed bool
}

// Wanted reports whether a message goes out under the When setting.
func (n Notify) Wanted(m Message) bool { return m.Failed || n.When == "always" }

// SendWebhook posts the message to an incoming webhook: Teams (a MessageCard), Slack-style ({"text"}) for others.
func SendWebhook(ctx context.Context, url string, m Message) error {
	if !strings.HasPrefix(url, "https://") {
		return errors.New("the webhook address must start with https://")
	}
	text := m.Text
	if len(m.Lines) > 0 {
		text += "\n\n- " + strings.Join(m.Lines, "\n- ")
	}
	var body any = map[string]string{"text": "*" + m.Title + "*\n" + text}
	if strings.Contains(url, "office.com") || strings.Contains(url, "logic.azure.com") || strings.Contains(url, "powerplatform") {
		color := "2EB67D"
		if m.Failed {
			color = "E01E5A"
		}
		body = map[string]any{"@type": "MessageCard", "@context": "https://schema.org/extensions", "themeColor": color, "summary": m.Title, "title": m.Title, "text": strings.ReplaceAll(text, "\n", "<br>")}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("the webhook answered %s", res.Status)
	}
	return nil
}

// SendMail sends the message to n.To through the mail server (STARTTLS on 587, TLS on 465).
func SendMail(ctx context.Context, n Notify, password string, m Message) error {
	if n.SMTPHost == "" || n.From == "" || len(n.To) == 0 {
		return errors.New("set the mail server, the sender and at least one recipient")
	}
	port := n.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(n.SMTPHost, strconv.Itoa(port))
	d := net.Dialer{Timeout: 20 * time.Second}
	var conn net.Conn
	var err error
	if port == 465 {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: n.SMTPHost})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, n.SMTPHost)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: n.SMTPHost}); err != nil {
				return err
			}
		}
	}
	if n.SMTPUser != "" {
		if err := c.Auth(smtp.PlainAuth("", n.SMTPUser, password, n.SMTPHost)); err != nil {
			return err
		}
	}
	if err := c.Mail(n.From); err != nil {
		return err
	}
	for _, t := range n.To {
		if err := c.Rcpt(strings.TrimSpace(t)); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	body := m.Text
	if len(m.Lines) > 0 {
		body += "\r\n\r\n" + strings.Join(m.Lines, "\r\n")
	}
	fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		n.From, strings.Join(n.To, ", "), mimeWord(m.Title), strings.ReplaceAll(body, "\n", "\r\n"))
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func mimeWord(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?utf-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
		}
	}
	return s
}
