package session

import "strings"

// Hop describes a session whose remote command is itself an ssh command,
// e.g. "ssh -t -i ~/.ssh/aws_key ec2-user@10.0.0.5" run on a jump station
// whose private key lives on that jump station.
type Hop struct {
	Target string // user@host as written in the command
	bin    string // "ssh" or the path used in the command
	opts   []string
}

// sshOptsWithArg are ssh options that take a value.
const sshOptsWithArg = "BbcDEeFIiJLlmOoPpQRSWw"

// ParseHop recognises "ssh [options] [user@]host [command]" in a remote command.
func ParseHop(cmd string) (Hop, bool) {
	words := splitWords(cmd)
	if len(words) < 2 {
		return Hop{}, false
	}
	if w := words[0]; w != "ssh" && !strings.HasSuffix(w, "/ssh") && w != "exec" {
		return Hop{}, false
	}
	if words[0] == "exec" {
		words = words[1:]
		if len(words) < 2 || (words[0] != "ssh" && !strings.HasSuffix(words[0], "/ssh")) {
			return Hop{}, false
		}
	}
	h := Hop{bin: words[0]}
	for i := 1; i < len(words); i++ {
		w := words[i]
		if !strings.HasPrefix(w, "-") || w == "-" {
			h.Target = w
			return h, h.Target != ""
		}
		if w == "--" {
			if i+1 < len(words) {
				h.Target = words[i+1]
			}
			return h, h.Target != ""
		}
		// a group of single-letter flags, possibly ending in one that takes a value: -tt, -At, -p2222, -i key
		flags := w[1:]
		var keep strings.Builder
		for j := 0; j < len(flags); j++ {
			f := flags[j]
			if strings.IndexByte(sshOptsWithArg, f) >= 0 {
				val := flags[j+1:]
				if val == "" {
					if i+1 >= len(words) {
						return Hop{}, false
					}
					i++
					val = words[i]
				}
				if keep.Len() > 0 {
					h.opts = append(h.opts, "-"+keep.String())
					keep.Reset()
				}
				h.opts = append(h.opts, "-"+string(f), val)
				break
			}
			if f == 't' || f == 'T' || f == 'N' || f == 'f' || f == 'n' || f == 's' || f == 'G' || f == 'V' {
				continue // tty / no-command flags are set by us
			}
			keep.WriteByte(f)
		}
		if keep.Len() > 0 {
			h.opts = append(h.opts, "-"+keep.String())
		}
	}
	return Hop{}, false
}

// command builds the ssh command run on the jump station. BatchMode makes it
// fail fast (instead of waiting for a password nobody can type) when the key
// does not work.
func (h Hop) command(tail ...string) string {
	parts := []string{shellWord(h.bin), "-T", "-o", "BatchMode=yes"}
	for _, o := range h.opts {
		parts = append(parts, shellWord(o))
	}
	parts = append(parts, tail...)
	return strings.Join(parts, " ")
}

// SFTPCommand runs the sftp subsystem on the target.
func (h Hop) SFTPCommand() string { return h.command("-s", shellWord(h.Target), "sftp") }

// TTYCommand runs a command on the target with a terminal (-tt) and no escape
// character, so an interactive "sudo su - user" can ask for a password.
func (h Hop) TTYCommand(cmd string) string {
	parts := []string{shellWord(h.bin), "-tt", "-e", "none", "-o", "BatchMode=yes"}
	for _, o := range h.opts {
		parts = append(parts, shellWord(o))
	}
	return strings.Join(append(parts, shellWord(h.Target), quote(cmd)), " ")
}

// ExecCommand runs a command on the target.
func (h Hop) ExecCommand(cmd string) string { return h.command(shellWord(h.Target), quote(cmd)) }

// splitWords splits a command line like a POSIX shell (quotes and backslashes),
// keeping a leading ~ so it is still expanded by the remote shell.
func splitWords(s string) []string {
	var out []string
	var cur strings.Builder
	in := false
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '\'':
			if c == '\'' {
				q = 0
			} else {
				cur.WriteByte(c)
			}
		case q == '"':
			if c == '"' {
				q = 0
			} else if c == '\\' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			q, in = c, true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			in = true
		case c == ' ' || c == '\t' || c == '\n':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		case c == ';' || c == '&' || c == '|':
			return append(out, cur.String()) // stop at the first shell operator
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out
}

// shellWord quotes a word for the remote shell unless it is plain; "~/x" keeps the ~ unquoted.
func shellWord(w string) string {
	plain := w != ""
	for _, c := range w {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_./=:@,+%~", c)) {
			plain = false
			break
		}
	}
	if plain {
		return w
	}
	if strings.HasPrefix(w, "~/") {
		return "~/" + quote(w[2:])
	}
	return quote(w)
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// HopOf returns the hop of a session that runs "ssh … host" as its remote command.
func HopOf(s Session) (Hop, bool) {
	if s.Protocol != ProtoSSH2 || !s.UseRemoteCommand {
		return Hop{}, false
	}
	return ParseHop(s.RemoteCommand)
}
