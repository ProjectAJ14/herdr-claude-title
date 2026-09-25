package title

import (
	"strings"

	"github.com/ProjectAJ14/herdr-claude-title/internal/session"
)

const sshKind = "ssh"

// sshFlagsWithValue take their value as the next argument unless attached,
// so `ssh -p 2222 prod-01` does not read 2222 as the destination.
var sshFlagsWithValue = func() map[byte]struct{} {
	m := make(map[byte]struct{})
	for _, c := range []byte("BbcDEeFIiJLlmOoPpQRSWw") {
		m[c] = struct{}{}
	}

	return m
}()

// remoteHost names the machine a pane reached: the place becomes
// `ssh › prod-01`. The mark goes in the place, which nothing outranks,
// rather than the activity the remote shell's own title would take over.
type remoteHost struct{}

func (remoteHost) Name() string { return "ssh" }
func (remoteHost) Rank() int    { return RankSSH }

func (remoteHost) Contribute(p *session.Pane) (Parts, bool) {
	args, running := sshArgs(p)
	if !running {
		return Parts{}, false
	}

	if host := Sanitize(sshDestination(args), 0); host != "" {
		return Parts{Place: sshKind + Separator + host}, true
	}

	return Parts{Place: sshKind}, true
}

// sshArgs is the argv of the ssh the pane itself runs. Only the foreground
// process counts: git, agents and jump hosts start ssh of their own.
func sshArgs(p *session.Pane) ([]string, bool) {
	proc, ok := p.Foreground()
	if !ok || !strings.EqualFold(proc.Name, sshKind) || sshIsTunnel(proc.Args) {
		return nil, false
	}

	return proc.Args, true
}

// echoesSSHCommand spots a title that is the ssh command line itself, by its
// first word: fish trims it to twenty columns (`ssh deploy@productio`).
func echoesSSHCommand(p *session.Pane, value string) bool {
	if _, running := sshArgs(p); !running {
		return false
	}

	first, _, _ := strings.Cut(strings.TrimSpace(value), " ")

	return strings.EqualFold(first, sshKind)
}

// sshIsTunnel reports -N before the destination (-pN is a port).
func sshIsTunnel(args []string) bool {
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "-" {
			continue
		}

		if len(arg) < 2 || arg[0] != '-' {
			return false
		}

		for j := 1; j < len(arg); j++ {
			if _, takesValue := sshFlagsWithValue[arg[j]]; takesValue {
				if j == len(arg)-1 {
					i++
				}

				break
			}

			if arg[j] == 'N' {
				return true
			}
		}
	}

	return false
}

// sshDestination is the first argument that is neither an option nor an
// option's value; what follows is the remote command.
func sshDestination(args []string) string {
	for i := 1; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--":
			if i+1 < len(args) {
				return hostOf(args[i+1])
			}

			return ""
		case arg == "-":
		case len(arg) > 1 && arg[0] == '-':
			if _, takesValue := sshFlagsWithValue[arg[len(arg)-1]]; takesValue {
				i++
			}
		default:
			return hostOf(arg)
		}
	}

	return ""
}

// hostOf drops scheme, user, path and port: `ssh://deploy@prod-01:2222`.
func hostOf(destination string) string {
	host := strings.TrimPrefix(strings.TrimSpace(destination), "ssh://")
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}

	if slash := strings.Index(host, "/"); slash >= 0 {
		host = host[:slash]
	}

	if strings.HasPrefix(host, "[") { // bracketed IPv6
		if end := strings.Index(host, "]"); end >= 0 {
			return host[1:end]
		}

		return ""
	}

	if strings.Count(host, ":") == 1 { // several colons: bare IPv6, no port
		host, _, _ = strings.Cut(host, ":")
	}

	return host
}
