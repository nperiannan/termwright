package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/secrets"
)

// The AI panel's chat: several turns, pictures, and the Agent mode, where the
// AI asks Termwright to run commands in tabs (the window runs them, after the
// user allows it) and reads the results in the next turn.

// AIImage is a picture the user pasted (base64, without the data: prefix).
type AIImage struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Data      string `json:"data"`
}

// AIMsg is one turn of the chat.
type AIMsg struct {
	Role   string    `json:"role"` // user | assistant
	Text   string    `json:"text"`
	Images []AIImage `json:"images,omitempty"`
}

// AIChatRequest is the whole chat so far; the last message is the user's.
type AIChatRequest struct {
	Messages   []AIMsg  `json:"messages"`
	DeviceType string   `json:"deviceType"`
	Protocol   string   `json:"protocol"`
	Agent      bool     `json:"agent"`
	Tabs       []string `json:"tabs"`           // agent: the open tabs, "name (device type, state)"
	Tools      []string `json:"tools"`          // agent: what it may do - "run" (commands in tabs), "open" (find and connect to saved sessions), "log", "record", "lab" (the emulated lab)
	Folders    []string `json:"folders"`        // agent: the saved-session folders
	Perm       string   `json:"perm"`           // agent: "" changes ask first, "every", "readonly", "auto" (fully autonomous)
	Plan       string   `json:"plan,omitempty"` // agent: "list" (a task of several steps gets a checklist plan first), "only" (Plan mode: look, plan, run nothing that changes)
}

const aiMaxImages = 4
const aiMaxImage = 5 << 20 // bytes, decoded

// AIChat sends the chat and returns the answer (with what was sent of the last turn).
func (a *App) AIChat(r AIChatRequest) (AIAnswer, error) {
	if err := a.ready(); err != nil {
		return AIAnswer{}, err
	}
	s := a.cfg.Get().AI
	if s.Provider == "" {
		return AIAnswer{}, errors.New("the AI assistant is off - choose an AI service in Settings → AI Assistant")
	}
	if len(r.Messages) == 0 || r.Messages[len(r.Messages)-1].Role != "user" {
		return AIAnswer{}, errors.New("nothing to ask")
	}
	// keep the end of a long chat
	total := 0
	start := 0
	for i := len(r.Messages) - 1; i >= 0; i-- {
		total += len(r.Messages[i].Text)
		if total > aiMaxText*2 && i < len(r.Messages)-1 {
			start = i + 1
			break
		}
	}
	msgs := append([]AIMsg(nil), r.Messages[start:]...)
	for len(msgs) > 0 && msgs[0].Role != "user" {
		msgs = msgs[1:]
	}
	// secrets out of every turn, the same [host-N] for a host everywhere
	const cut = "\n␞␞\n"
	var all []string
	for _, m := range msgs {
		t := m.Text
		if len(t) > aiMaxText {
			t = "…\n" + t[len(t)-aiMaxText:]
		}
		all = append(all, t)
	}
	joined, removed := aiRedact(strings.Join(all, cut), s.HideHosts)
	parts := strings.Split(joined, cut)
	if len(parts) != len(msgs) {
		return AIAnswer{}, errors.New("the text could not be prepared for sending")
	}
	nImg := 0
	for i := range msgs {
		msgs[i].Text = parts[i]
		var keep []AIImage
		for _, im := range msgs[i].Images {
			if nImg >= aiMaxImages || base64.StdEncoding.DecodedLen(len(im.Data)) > aiMaxImage || !strings.HasPrefix(im.MediaType, "image/") {
				continue
			}
			keep = append(keep, im)
			nImg++
		}
		msgs[i].Images = keep
	}
	key, _ := secrets.Lookup(aiKeyName + s.Provider)
	if aiNeedsKey(s.Provider) && key == "" {
		return AIAnswer{}, errors.New("add the API key in Settings → AI Assistant")
	}
	model := s.Model
	if model == "" && s.Provider != "claudecode" {
		ms, err := a.AIModels(s)
		if err != nil || len(ms) == 0 {
			return AIAnswer{}, fmt.Errorf("choose a model in Settings → AI Assistant (%v)", err)
		}
		model = ms[0]
	}
	wait := 2 * time.Minute
	if s.Provider == "claudecode" {
		wait = claudeCodeTimeout + 10*time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	sys := aiChatSystem(r)
	if r.Agent && slices.Contains(r.Tools, "lab") {
		lab, _ := aiRedact(a.emuAgentLab(), s.HideHosts) // the same as the chat: secrets out, hosts hidden when set
		sys += lab
	}
	out, usage, err := aiConverse(ctx, s, key, model, sys, msgs)
	if err != nil {
		return AIAnswer{}, err
	}
	if usage != nil && usage.Model != "" {
		model = usage.Model
	} else if model == "" {
		model = "Claude Code"
	}
	return AIAnswer{Text: out, Commands: aiCommands(out), Sent: msgs[len(msgs)-1].Text, Removed: removed, Model: model, Usage: usage}, nil
}

func aiChatSystem(r AIChatRequest) string {
	sys, _ := aiPrompt(AIRequest{Kind: "explain", DeviceType: r.DeviceType, Protocol: r.Protocol}, "")
	sys += "\nThe user may paste pictures (screenshots, photos of devices); describe what matters in them."
	on := map[string]bool{}
	for _, t := range r.Tools {
		on[t] = true
	}
	if len(r.Tools) == 0 { // older windows: commands and sessions
		on["run"], on["open"] = true, true
	}
	if !r.Agent || (!on["run"] && !on["open"]) {
		return sys
	}
	tabs := "none"
	if len(r.Tabs) > 0 {
		tabs = strings.Join(r.Tabs, "; ")
	}
	var lines, what []string
	if on["run"] {
		lines, what = append(lines, "run <tab name>: <one command line>"), append(what, "run commands in the open tabs")
	}
	if on["open"] {
		lines = append(lines, "find <text>", "open <saved session: its name, folder/name, or address>")
		what = append(what, "find saved sessions (by folder, name, address or device type) and connect to them")
	}
	if on["log"] {
		lines, what = append(lines, "log <tab name>: on|off"), append(what, "log a tab to a file")
	}
	if on["record"] {
		lines, what = append(lines, "record <tab name>: on|off|mark <note>"), append(what, "record a tab (a replayable recording with markers)")
	}
	out := sys + `

AGENT MODE. Through Termwright you can ` + strings.Join(what, ", ") + `. To do so, end your answer with one block:
` + "```termwright\n" + strings.Join(lines, "\n") + "\n```" + `
Termwright does them in that order and sends you the results in the next message. Then continue, or give your conclusion with no block.
Rules: one command per run line, exactly as typed at that device's prompt; prefer read-only commands (show, display, ping, traceroute) to find the cause; propose configuration changes only when the user asked for a fix or a configuration, and say what each does before the block; never save the configuration, reboot or erase unless the user asked for exactly that; turn off paging first if the output pages (EXOS: disable clipaging, VOSS: terminal more disable, Cisco: terminal length 0).`
	if on["open"] {
		out += `
Saved sessions log in with the credentials stored in Termwright: never ask for, show or type passwords. For a group of devices (a location, a folder, an address range) use find first, then open each one you need.`
		if len(r.Folders) > 0 {
			f := r.Folders
			if len(f) > 80 {
				f = f[:80]
			}
			out += "\nSaved session folders: " + strings.Join(f, "; ") + "."
		}
	}
	if on["log"] || on["record"] {
		out += `
To keep evidence, turn on log (and record, when it helps to replay the session) on each tab before you debug or change it, and turn them off when you finish; Termwright tells you the file names - tell the user.`
	}
	switch r.Plan {
	case "list":
		out += `
PLAN. For a task of three or more steps, put your plan first in the block, a line per step (at most 8, each a few words): plan [ ] <step>. In each later answer repeat all the plan lines, [x] for the steps done and [~] for the one you are doing now, until they are all [x]. A question or a single step needs no plan.`
	case "only":
		out += `
PLAN MODE. Look before you plan: only find, open and read-only commands run now (anything else is refused). Then give the plan in the block, a line per step (at most 8): plan [ ] <step>, and above the block what each step does and the commands it will use - then stop. The user reads it and presses Run the Plan; then carry it out step by step, repeating the plan lines with [x] for the steps done and [~] for the one you are doing.`
	}
	switch r.Perm {
	case "auto":
		out += `
The user let you work autonomously: commands and changes run without asking, except on production sessions and for reboot, reload or erase. Work in small steps, check each change with a show command before the next device, stop and report if something fails, and end with a short status table: device, what was done, result.`
	case "readonly":
		out += `
The user allows only read-only commands: give configuration changes as commands for the user, do not run them.`
	default:
		out += `
Each command that is not read-only waits for the user to allow it.`
	}
	return out + "\nOpen tabs: " + tabs + "."
}

// aiConverse sends a chat to the provider.
func aiConverse(ctx context.Context, s config.AISettings, key, model, sys string, msgs []AIMsg) (string, *AIUsage, error) {
	switch s.Provider {
	case "claudecode":
		return claudeCodeConverse(ctx, s.BaseURL, model, s.Effort, sys, msgs)
	case "anthropic":
		var ms []map[string]any
		for _, m := range msgs {
			var content []map[string]any
			for _, im := range m.Images {
				content = append(content, map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": im.MediaType, "data": im.Data}})
			}
			content = append(content, map[string]any{"type": "text", "text": nonEmpty(m.Text)})
			ms = append(ms, map[string]any{"role": m.Role, "content": content})
		}
		body := map[string]any{"model": model, "max_tokens": 2500, "system": sys, "messages": ms}
		var v struct {
			Model   string `json:"model"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Usage struct {
				In     int `json:"input_tokens"`
				Out    int `json:"output_tokens"`
				CacheR int `json:"cache_read_input_tokens"`
				CacheW int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if err := aiPost(ctx, "https://api.anthropic.com/v1/messages", map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}, body, &v); err != nil {
			return "", nil, err
		}
		var b strings.Builder
		for _, c := range v.Content {
			if c.Type == "text" {
				b.WriteString(c.Text)
			}
		}
		return b.String(), &AIUsage{In: v.Usage.In + v.Usage.CacheR + v.Usage.CacheW, Out: v.Usage.Out, Model: v.Model}, nil
	case "openai":
		ms := []map[string]any{{"role": "system", "content": sys}}
		for _, m := range msgs {
			if len(m.Images) == 0 {
				ms = append(ms, map[string]any{"role": m.Role, "content": nonEmpty(m.Text)})
				continue
			}
			content := []map[string]any{{"type": "text", "text": nonEmpty(m.Text)}}
			for _, im := range m.Images {
				content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + im.MediaType + ";base64," + im.Data}})
			}
			ms = append(ms, map[string]any{"role": m.Role, "content": content})
		}
		body := map[string]any{"model": model, "messages": ms}
		var v struct {
			Model   string `json:"model"`
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage *struct {
				In  int `json:"prompt_tokens"`
				Out int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := aiPost(ctx, aiBase(s)+"/v1/chat/completions", map[string]string{"Authorization": "Bearer " + key}, body, &v); err != nil {
			return "", nil, err
		}
		if len(v.Choices) == 0 {
			return "", nil, errors.New("the AI service gave no answer")
		}
		var u *AIUsage
		if v.Usage != nil {
			u = &AIUsage{In: v.Usage.In, Out: v.Usage.Out}
		}
		return v.Choices[0].Message.Content, u, nil
	case "ollama":
		ms := []map[string]any{{"role": "system", "content": sys}}
		for _, m := range msgs {
			mm := map[string]any{"role": m.Role, "content": nonEmpty(m.Text)}
			if len(m.Images) > 0 {
				var imgs []string
				for _, im := range m.Images {
					imgs = append(imgs, im.Data)
				}
				mm["images"] = imgs
			}
			ms = append(ms, mm)
		}
		body := map[string]any{"model": model, "stream": false, "messages": ms}
		var v struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			In  int `json:"prompt_eval_count"`
			Out int `json:"eval_count"`
		}
		if err := aiPost(ctx, aiBase(s)+"/api/chat", nil, body, &v); err != nil {
			return "", nil, err
		}
		return v.Message.Content, &AIUsage{In: v.In, Out: v.Out}, nil
	}
	return "", nil, errors.New("unknown AI service " + s.Provider)
}

func nonEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(see the picture)"
	}
	return s
}

// claudeCodeConverse: Claude Code answers one prompt, so the chat goes in as a
// transcript; pictures are files in a fresh folder it may read (and nothing else).
func claudeCodeConverse(ctx context.Context, path, model, effort, sys string, msgs []AIMsg) (string, *AIUsage, error) {
	var b strings.Builder
	var dir string
	n := 0
	for i, m := range msgs {
		if len(msgs) > 1 {
			if i == len(msgs)-1 {
				b.WriteString("\n\nNow, my message:\n")
			} else if m.Role == "user" {
				b.WriteString("\n\n[Earlier - me]\n")
			} else {
				b.WriteString("\n\n[Earlier - you]\n")
			}
		}
		b.WriteString(m.Text)
		for _, im := range m.Images {
			raw, err := base64.StdEncoding.DecodeString(im.Data)
			if err != nil {
				continue
			}
			if dir == "" {
				if dir, err = os.MkdirTemp("", "termwright-ai-"); err != nil {
					return "", nil, err
				}
				defer os.RemoveAll(dir)
			}
			n++
			ext := ".png"
			switch im.MediaType {
			case "image/jpeg":
				ext = ".jpg"
			case "image/gif":
				ext = ".gif"
			case "image/webp":
				ext = ".webp"
			}
			name := fmt.Sprintf("picture-%d%s", n, ext)
			if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
				return "", nil, err
			}
			fmt.Fprintf(&b, "\n[Picture: %s - read it with the Read tool]", filepath.Join(dir, name))
		}
	}
	return claudeCodeRun(ctx, path, model, effort, sys, b.String(), dir)
}

// The AI panel's chats (and where the panel sits) are kept in the data folder,
// so backups and restores include them. The device output sent with a
// question is not stored.
const aiChatsFile = "ai-chats.json"

// AIChatsLoad returns what AIChatsSave stored ("" when nothing yet).
func (a *App) AIChatsLoad() (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(a.cfg.DataDir(), aiChatsFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// AIChatsSave stores the chats (JSON, at most 8 MB), readable only by you.
func (a *App) AIChatsSave(data string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return errors.New("the chats are too large to keep")
	}
	if !json.Valid([]byte(data)) {
		return errors.New("not JSON")
	}
	dst := filepath.Join(a.cfg.DataDir(), aiChatsFile)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
