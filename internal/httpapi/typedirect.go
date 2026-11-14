package httpapi

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/applog"
	"keel/internal/cmdrule"
	"keel/internal/runtime"
	"keel/internal/sessionsock"
	"keel/internal/wire"
)

// Type without the approve dialog (docs/ui-redesign.md §6 rule 13, for
// review). On the session socket the window sends:
//
//	{"type":"inject", "command", "nonce", "direct":{…}, "agent":{…}}
//	    → {"type":"type-direct", "ref", "ok", "dialog", "reason", "message"}
//
// An inject with "direct" runs the checks at the moment of the write, on
// the exact bytes, then
// consumes the server nonce and writes the bytes: the command alone, never
// a newline, so it waits on the prompt line for the user's Enter. When
// any check is not met the answer is dialog:true and nothing is written:
// the window then opens the approve dialog.

// The "direct" part of an inject is wire.DirectType: where the command
// comes from (an agent proposal, with the frame's "agent" part, a snippet
// or a notebook's block) and what the window's terminal shows.

// Why a direct type goes to the dialog, beyond the agent service's own.
const (
	reasonAltScreen  = "the pane shows a full-screen program (such as vim, less or htop), where typed bytes are keystrokes"
	reasonUnknown    = "Keel cannot check this command here"
	reasonUnreviewed = "the agent edited this notebook and you have not reviewed that edit"
	reasonNotPrompt  = "the shell is not at its prompt (a command is still running)"
	reasonInput      = "the pane looks like it is asking for input (a password, a confirmation, another program's prompt)"
	reasonBusy       = "too many checks at once"
)

// inputPromptRe is a last screen line that asks for input rather than a
// command: typed bytes there would answer it (an echo-off password prompt
// above all). Case-insensitive, on the last line with spaces trimmed.
var inputPromptRe = regexp.MustCompile(`(?i)pass(word|phrase|code)|\bpin\b|one-time|verification code|\botp\b|\[sudo\]|'s password|are you sure|\[y/n\]|\(y/n\)|\(yes/no|\[yes/no|\byes/no\b|\b(continue|proceed|overwrite|replace|delete)\?|\benter\b[^:]*:$|\b(login|username|user name|token|passcode|secret|key)\s*:$|^(mysql|mariadb|sqlite|redis[^>]*|ftp|sftp|irb[^>]*|\(gdb\)|\(pdb\)|>>>|\.\.\.|in \[\d+\]:)\s*>?$|^[\w-]+[=-][#>]$|^>$`)

func looksLikeInput(line string) bool {
	line = strings.TrimSpace(line)
	return line != "" && inputPromptRe.MatchString(line)
}

// promptCheck sends a direct type to the dialog unless the pane is at its
// shell's prompt: with OSC 133 marks, at a prompt with no command open;
// in any case, not on a line that asks for input (keel serve's view of the
// output tail, or the window's own line).
func promptCheck(sess *runtime.Session, d *wire.DirectType) error {
	marks, atPrompt, last := sess.PromptState()
	if marks && !atPrompt {
		return &service.DialogError{Reason: reasonNotPrompt}
	}
	if looksLikeInput(last) || looksLikeInput(d.PromptLine) {
		return &service.DialogError{Reason: reasonInput}
	}
	return nil
}

// directLimiter bounds direct types on one socket: a bucket of
// directBurst for each kind, refilled at one every directRefill. A
// faster page is misbehaving; it is answered "dialog" and nothing reaches
// a provider.
type directLimiter struct {
	mu     sync.Mutex
	tokens map[string]float64
	at     map[string]time.Time
}

const (
	directBurst  = 20
	directRefill = 200 * time.Millisecond
)

func newDirectLimiter() *directLimiter {
	return &directLimiter{tokens: map[string]float64{}, at: map[string]time.Time{}}
}

func (l *directLimiter) allow(kind string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	tok, ok := l.tokens[kind]
	if !ok {
		tok = directBurst
	} else {
		tok = min(directBurst, tok+float64(now.Sub(l.at[kind]))/float64(directRefill))
	}
	l.at[kind] = now
	if tok < 1 {
		l.tokens[kind] = tok
		return false
	}
	l.tokens[kind] = tok - 1
	return true
}

// shellLangs are the notebook fence languages that offer Type (the editor's
// SHELL_LANGS).
var shellLangs = map[string]bool{"sh": true, "bash": true, "shell": true}

// notebookLine reports whether command is, exactly, the one line of a
// shell code fence in body: a fence with one non-empty line whose text,
// trimmed, is command. Prose and longer blocks do not count.
func notebookLine(body, command string) bool {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		open := strings.TrimLeft(lines[i], " ")
		if len(lines[i])-len(open) > 3 {
			continue
		}
		var ch string
		switch {
		case strings.HasPrefix(open, "```"):
			ch = "`"
		case strings.HasPrefix(open, "~~~"):
			ch = "~"
		default:
			continue
		}
		n := len(open) - len(strings.TrimLeft(open, ch))
		info := strings.Fields(strings.TrimSpace(open[n:]))
		lang := ""
		if len(info) > 0 {
			lang = strings.ToLower(info[0])
		}
		var block []string
		j := i + 1
		for ; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if len(t) >= n && strings.Trim(t, ch) == "" {
				break
			}
			block = append(block, lines[j])
		}
		i = j
		for len(block) > 0 && strings.TrimSpace(block[len(block)-1]) == "" {
			block = block[:len(block)-1]
		}
		for len(block) > 0 && strings.TrimSpace(block[0]) == "" {
			block = block[1:]
		}
		if shellLangs[lang] && len(block) == 1 && strings.TrimSpace(block[0]) == command {
			return true
		}
	}
	return false
}

// errFollowing refuses a direct type from a client that does not drive.
var errFollowing = errors.New("you follow this pane: take or request control to type")

// errJoined refuses a direct type into a pane a share peer drives.
var errJoined = errors.New("this pane is joined from another Keel: type into it there")

// directCheck runs every check of a type without the dialog for this
// client of sess. A *service.DialogError sends it to the dialog; another
// error refuses it. An agent proposal's agent.type is logged (and a group
// run's host claimed) as the step before the write.
func (s *Server) directCheck(ctx context.Context, sess *runtime.Session, clientID uint64, command string, d *wire.DirectType, am *wire.AgentType) (service.DirectGrant, error) {
	var none service.DirectGrant
	info := sess.Info()
	if info.Role(clientID) != runtime.RoleDriver {
		return none, errFollowing
	}
	// A joined pane (the driver is a share peer) is refused here too, not
	// only in the window.
	if sess.DrivenByJoined() {
		return none, errJoined
	}
	if sess.AltScreen() || d.AltScreen {
		return none, &service.DialogError{Reason: reasonAltScreen}
	}
	if err := promptCheck(sess, d); err != nil {
		return none, err
	}
	switch d.Kind {
	case wire.DirectAgent:
		if am == nil || s.agent == nil {
			return none, &service.DialogError{Reason: reasonUnknown}
		}
		return s.agent.AuthorizeDirect(ctx, service.TypeRequest{
			ConversationID: am.ConversationID,
			GroupRunID:     am.GroupRunID,
			ProposalID:     am.ProposalID,
			SessionID:      info.ID,
			Role:           sess.Role(clientID),
			Bytes:          command,
		})
	case wire.DirectSnippet:
		// A snippet is the user's own text: one with a placeholder goes
		// to the dialog, as before, rather than being refused.
		if !cmdrule.DirectLine(command) || cmdrule.HasPlaceholder(command) {
			return none, &service.DialogError{Reason: service.ReasonLines}
		}
		snippets, err := s.store.ListSnippets()
		if err != nil {
			return none, &service.DialogError{Reason: service.ReasonBytes}
		}
		for _, sn := range snippets {
			if sn.ID != d.SnippetID {
				continue
			}
			if strings.TrimRight(sn.Command, "\r\n") != command {
				break
			}
			// Snippet bodies are vault contents: Jev rates one only when
			// the user marked it agent-usable (§6 rule 3).
			return none, s.agent.DirectRating(ctx, info.ID, command, sn.AgentUsable)
		}
		return none, &service.DialogError{Reason: service.ReasonBytes}
	case wire.DirectNotebook:
		// A placeholder is a shell glob, never the value (rule 9).
		if cmdrule.HasPlaceholder(command) {
			return none, service.ErrPlaceholder
		}
		if !cmdrule.DirectLine(command) {
			return none, &service.DialogError{Reason: service.ReasonLines}
		}
		nb, err := s.store.GetNotebook(d.NotebookID)
		if err != nil {
			return none, &service.DialogError{Reason: service.ReasonBytes}
		}
		// The dialog says so above the exact bytes (§6 rule 12).
		if nb.AgentUnreviewed {
			return none, &service.DialogError{Reason: reasonUnreviewed}
		}
		// The command must be a shell code block of exactly one line,
		// not text found anywhere (prose, a longer block).
		found := false
		for _, c := range nb.Cells {
			found = found || notebookLine(c.Body, command)
		}
		if !found {
			return none, &service.DialogError{Reason: service.ReasonBytes}
		}
		// A private notebook does not exist for the agent (§6 rule 12):
		// its blocks are never sent to Jev.
		return none, s.agent.DirectRating(ctx, info.ID, command, !nb.Private)
	}
	return none, &service.DialogError{Reason: reasonUnknown}
}

// typeDirect is an inject with "direct": the checks, then the write of
// the command alone with the server nonce, and the log entry.
func (s *Server) typeDirect(ctx context.Context, c *sessionsock.Conn, command, nonce string, d *wire.DirectType, am *wire.AgentType) {
	out := wire.TypeResult{Type: wire.TypeTypeDirect, Ref: d.Ref}
	sess, clientID := c.Session(), c.ClientID()
	info := sess.Info()
	grant, err := s.directCheck(ctx, sess, clientID, command, d, am)
	if err != nil {
		if reason, ok := service.NeedsDialog(err); ok {
			out.Dialog, out.Reason = true, reason
		} else {
			out.Message = err.Error()
		}
		c.Send(out)
		return
	}
	group := d.Kind == wire.DirectAgent && am.GroupRunID != ""
	// Never a newline: the command waits on the prompt line.
	command = strings.TrimRight(command, "\r\n")
	mark := sess.StreamMark()
	if err := sess.InjectTicket(clientID, command, nonce); err != nil {
		if group {
			s.agent.GroupTypeFailed(am.GroupRunID, am.ProposalID)
		}
		s.note(logKind(d.Kind), "a command was not typed: "+err.Error(), info.HostID, info.ID, "")
		out.Message = "the type expired; press Type again"
		if errors.Is(err, runtime.ErrNotReady) {
			out.Message = notReadyTypeMessage
		}
		c.Send(out)
		return
	}
	switch {
	case group:
		// agent.type (Source "direct") only now that the bytes are written.
		s.agent.DirectTyped(grant)
		s.agent.GroupTypeDoneAt(am.GroupRunID, am.ProposalID, mark)
	case d.Kind == wire.DirectAgent:
		s.agent.DirectTyped(grant)
		s.agent.TypeDoneAt(am.ConversationID, am.ProposalID, mark)
	default:
		// A snippet or a notebook block: its id and counts, never its text.
		if s.logs != nil {
			id := d.SnippetID
			if d.Kind == wire.DirectNotebook {
				id = d.NotebookID
			}
			s.logs.Append(applog.Entry{
				Kind:      d.Kind,
				Message:   fmt.Sprintf("%s %s typed without Enter into %s · %d bytes, 1 line · direct", d.Kind, id, paneName(info), len(command)),
				HostID:    info.HostID,
				SessionID: info.ID,
				Lines:     1,
				Bytes:     len(command),
				Source:    agent.TypeDirect,
			})
		}
	}
	out.OK = true
	c.Send(out)
}

func logKind(kind string) string {
	if kind == wire.DirectAgent {
		return wire.LogAgent
	}
	return kind
}

func paneName(info runtime.Info) string {
	if info.Title != "" {
		return info.Title
	}
	return "a pane"
}
