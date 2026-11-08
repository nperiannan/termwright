package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/secrets"
)

// The AI assistant (optional, off by default): Explain output or an error,
// and Suggest a command for a device type. It uses the provider and key the
// user chose - Claude (Anthropic), an OpenAI-compatible endpoint or a local
// Ollama - and sends only what the user picked, with secrets removed first.
// Nothing it answers is run: commands are pasted without Enter.

const aiKeyName = "termwright:ai-key:" // + provider, in the OS keychain

// AIRequest: what the user asks.
type AIRequest struct {
	Kind       string `json:"kind"`       // "explain" (Text is output or an error) or "suggest" (Text is what to do)
	Text       string `json:"text"`       // the selection or the question
	DeviceType string `json:"deviceType"` // the session's device type ("" = unknown)
	Protocol   string `json:"protocol"`
}

// AIAnswer: the answer and what was sent.
type AIAnswer struct {
	Text     string   `json:"text"`     // Markdown: paragraphs and ``` blocks
	Commands []string `json:"commands"` // the commands in the answer's code blocks
	Sent     string   `json:"sent"`     // the text as sent, after removing secrets
	Removed  int      `json:"removed"`  // how many secrets were removed
	Model    string   `json:"model"`
}

// AIStatus tells the UI whether the assistant is set up.
type AIStatus struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	HasKey   bool   `json:"hasKey"`
	Ready    bool   `json:"ready"`
}

func aiNeedsKey(p string) bool { return p == "anthropic" || p == "openai" }

// AIState: the assistant's setup (never the key itself).
func (a *App) AIState() AIStatus {
	s := a.cfg.Get().AI
	k, _ := secrets.Lookup(aiKeyName + s.Provider)
	st := AIStatus{Provider: s.Provider, Model: s.Model, HasKey: k != ""}
	st.Ready = s.Provider != "" && (!aiNeedsKey(s.Provider) || st.HasKey)
	return st
}

// AISetKey saves (or with "" forgets) the API key of a provider in the OS keychain.
func (a *App) AISetKey(provider, key string) error {
	if !aiNeedsKey(provider) {
		return errors.New("this provider takes no key")
	}
	if strings.TrimSpace(key) == "" {
		return secrets.Delete(aiKeyName + provider)
	}
	return secrets.Set(aiKeyName+provider, strings.TrimSpace(key))
}

// AIModels lists the provider's models, newest first where the provider says.
func (a *App) AIModels(s config.AISettings) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	key, _ := secrets.Lookup(aiKeyName + s.Provider)
	switch s.Provider {
	case "anthropic":
		var v struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := aiGet(ctx, "https://api.anthropic.com/v1/models?limit=100", map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}, &v); err != nil {
			return nil, err
		}
		out := []string{}
		for _, m := range v.Data {
			out = append(out, m.ID)
		}
		return out, nil
	case "openai":
		var v struct {
			Data []struct {
				ID      string `json:"id"`
				Created int64  `json:"created"`
			} `json:"data"`
		}
		if err := aiGet(ctx, aiBase(s)+"/v1/models", map[string]string{"Authorization": "Bearer " + key}, &v); err != nil {
			return nil, err
		}
		sort.Slice(v.Data, func(i, j int) bool { return v.Data[i].Created > v.Data[j].Created })
		out := []string{}
		for _, m := range v.Data {
			if !regexp.MustCompile(`(?i)embed|whisper|tts|dall-e|image|audio|moderation|realtime|transcribe|search`).MatchString(m.ID) {
				out = append(out, m.ID)
			}
		}
		return out, nil
	case "ollama":
		var v struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := aiGet(ctx, aiBase(s)+"/api/tags", nil, &v); err != nil {
			return nil, err
		}
		out := []string{}
		for _, m := range v.Models {
			out = append(out, m.Name)
		}
		return out, nil
	}
	return nil, errors.New("choose an AI service in Settings → AI Assistant")
}

func aiBase(s config.AISettings) string {
	b := strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	switch {
	case b != "":
		return strings.TrimSuffix(b, "/v1")
	case s.Provider == "ollama":
		return "http://localhost:11434"
	}
	return "https://api.openai.com"
}

// AIAsk sends the request to the chosen provider.
func (a *App) AIAsk(r AIRequest) (AIAnswer, error) {
	if err := a.ready(); err != nil {
		return AIAnswer{}, err
	}
	s := a.cfg.Get().AI
	if s.Provider == "" {
		return AIAnswer{}, errors.New("the AI assistant is off - choose an AI service in Settings → AI Assistant")
	}
	text := strings.TrimSpace(r.Text)
	if text == "" {
		return AIAnswer{}, errors.New("nothing to ask")
	}
	if len(text) > 24000 { // the end of a long selection matters most (the error, the prompt)
		text = "…\n" + text[len(text)-24000:]
	}
	sent, removed := aiRedact(text, s.HideHosts)
	key, _ := secrets.Lookup(aiKeyName + s.Provider)
	if aiNeedsKey(s.Provider) && key == "" {
		return AIAnswer{}, errors.New("add the API key in Settings → AI Assistant")
	}
	model := s.Model
	if model == "" {
		ms, err := a.AIModels(s)
		if err != nil || len(ms) == 0 {
			return AIAnswer{}, fmt.Errorf("choose a model in Settings → AI Assistant (%v)", err)
		}
		model = ms[0]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sys, user := aiPrompt(r, sent)
	out, err := aiChat(ctx, s, key, model, sys, user)
	if err != nil {
		return AIAnswer{}, err
	}
	return AIAnswer{Text: out, Commands: aiCommands(out), Sent: sent, Removed: removed, Model: model}, nil
}

func aiPrompt(r AIRequest, text string) (string, string) {
	dev := r.DeviceType
	if dev == "" {
		dev = "unknown (say which platforms your answer is for)"
	}
	sys := "You help a network engineer at the terminal of a device. Device type: " + dev + ". Protocol: " + r.Protocol + ".\n" +
		"Be brief and practical. Put every command for the device in its own fenced code block, one command per line, exactly as typed at the device's prompt. " +
		"Only give commands that exist on that platform; if you are not sure a command exists in this version, say so. " +
		"Prefer show (read-only) commands; mark any command that changes the configuration or may interrupt traffic with a warning before it. " +
		"Parts of the text were replaced with [secret] or [host-N] before it was sent to you; keep them as they are."
	switch r.Kind {
	case "explain":
		return sys, "Explain this output or error from the device: what it means, whether something is wrong, and what to check or do next.\n\n```\n" + text + "\n```"
	default:
		return sys, "What I want to do: " + text + "\nGive the command or commands for this device."
	}
}

func aiChat(ctx context.Context, s config.AISettings, key, model, sys, user string) (string, error) {
	switch s.Provider {
	case "anthropic":
		body := map[string]any{"model": model, "max_tokens": 1500, "system": sys, "messages": []map[string]string{{"role": "user", "content": user}}}
		var v struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := aiPost(ctx, "https://api.anthropic.com/v1/messages", map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}, body, &v); err != nil {
			return "", err
		}
		var b strings.Builder
		for _, c := range v.Content {
			if c.Type == "text" {
				b.WriteString(c.Text)
			}
		}
		return b.String(), nil
	case "openai":
		body := map[string]any{"model": model, "messages": []map[string]string{{"role": "system", "content": sys}, {"role": "user", "content": user}}}
		var v struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := aiPost(ctx, aiBase(s)+"/v1/chat/completions", map[string]string{"Authorization": "Bearer " + key}, body, &v); err != nil {
			return "", err
		}
		if len(v.Choices) == 0 {
			return "", errors.New("the AI service gave no answer")
		}
		return v.Choices[0].Message.Content, nil
	case "ollama":
		body := map[string]any{"model": model, "stream": false, "messages": []map[string]string{{"role": "system", "content": sys}, {"role": "user", "content": user}}}
		var v struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := aiPost(ctx, aiBase(s)+"/api/chat", nil, body, &v); err != nil {
			return "", err
		}
		return v.Message.Content, nil
	}
	return "", errors.New("unknown AI service " + s.Provider)
}

var aiHTTP = &http.Client{Timeout: 100 * time.Second}

func aiGet(ctx context.Context, url string, h map[string]string, out any) error {
	return aiDo(ctx, http.MethodGet, url, h, nil, out)
}

func aiPost(ctx context.Context, url string, h map[string]string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return aiDo(ctx, http.MethodPost, url, h, b, out)
}

func aiDo(ctx context.Context, method, url string, h map[string]string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range h {
		req.Header.Set(k, v)
	}
	resp, err := aiHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("the AI service cannot be reached: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error any `json:"error"`
		}
		msg := resp.Status
		if json.Unmarshal(b, &e) == nil && e.Error != nil {
			switch x := e.Error.(type) {
			case string:
				msg = x
			case map[string]any:
				if m, ok := x["message"].(string); ok {
					msg = m
				}
			}
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			msg += " - check the API key in Settings → AI Assistant"
		}
		return fmt.Errorf("the AI service answered: %s", msg)
	}
	return json.Unmarshal(b, out)
}

var (
	aiFence = regexp.MustCompile("(?s)```[a-zA-Z0-9_+-]*\n(.*?)```")
	// secrets in device output and configurations
	aiSecretRes = []*regexp.Regexp{
		regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
		regexp.MustCompile(`(?i)\b((?:enable |)(?:secret|password|passwd|pass|key-string|pre-shared-key|psk|passphrase|auth-key|authentication-key|md5|sha|token|api[-_]?key|secret-key)(?:[ \t]+(?:0|5|7|8|9|encrypted|hidden|cipher|plain|plaintext|text))?)([ \t]+|[ \t]*[:=][ \t]*)("[^"]*"|[^\s"]+)`),
		regexp.MustCompile(`(?i)\b(snmp-server community|snmp community|configure snmp add community(?:[ \t]+(?:readonly|readwrite|encrypted))?|community-string)([ \t]+)(\S+)`),
		regexp.MustCompile(`\b(AKIA|ASIA)[A-Z0-9]{16}\b`),
		regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{16,}|xox[abp]-[A-Za-z0-9-]{10,}|gh[pousr]_[A-Za-z0-9]{20,})\b`),
		regexp.MustCompile(`\b[A-Za-z0-9+/]{40,}={0,2}\b`), // long keys and hashes
		regexp.MustCompile(`\$\d\$[^\s$]+\$[^\s]+`),        // crypt hashes ($1$…, $6$…)
	}
	aiIPv4 = regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}\b`)
	aiIPv6 = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){2,7}[0-9a-f]{1,4}\b`)
	aiMAC  = regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?:[:-][0-9a-f]{2}){5}\b|\b[0-9a-f]{4}\.[0-9a-f]{4}\.[0-9a-f]{4}\b`)
	aiFQDN = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+(?:com|net|org|local|lan|corp|internal|io|in|de|uk|eu|cloud|int)\b`)
)

// aiRedact removes passwords, keys, SNMP communities and tokens; with
// hideHosts also addresses and host names (the same one gets the same
// placeholder). It returns the text and how many were replaced.
func aiRedact(text string, hideHosts bool) (string, int) {
	n := 0
	for i, re := range aiSecretRes {
		text = re.ReplaceAllStringFunc(text, func(m string) string {
			n++
			if i == 1 || i == 2 { // keep the keyword: "password [secret]"
				sub := re.FindStringSubmatch(m)
				if len(sub) >= 4 {
					return sub[1] + sub[2] + "[secret]"
				}
			}
			return "[secret]"
		})
	}
	if hideHosts {
		seen := map[string]string{}
		repl := func(m string) string {
			if _, ok := seen[m]; !ok {
				seen[m] = "[host-" + strconv.Itoa(len(seen)+1) + "]"
				n++
			}
			return seen[m]
		}
		for _, re := range []*regexp.Regexp{aiMAC, aiIPv6, aiIPv4, aiFQDN} {
			text = re.ReplaceAllStringFunc(text, repl)
		}
	}
	return text, n
}

// aiCommands: the lines of the answer's code blocks.
func aiCommands(answer string) []string {
	out := []string{}
	for _, m := range aiFence.FindAllStringSubmatch(answer, -1) {
		for _, l := range strings.Split(m[1], "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") && !strings.HasPrefix(l, "//") {
				out = append(out, l)
			}
		}
	}
	return out
}
