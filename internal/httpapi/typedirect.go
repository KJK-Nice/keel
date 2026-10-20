package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/applog"
	"keel/internal/runtime"
)

// Type without the approve dialog (docs/ui-redesign.md §6 rule 13, for
// review). On the session socket the page sends:
//
//	{"type":"type-check", "command", "direct":{…}, "agent":{…}}
//	    → {"type":"type-check", "ref", "direct", "dialog", "reason", "message"}
//	{"type":"inject", "command", "nonce", "direct":{…}, "agent":{…}}
//	    → {"type":"type-direct", "ref", "ok", "dialog", "reason", "message"}
//
// type-check writes nothing (the group run page asks it for every host
// before one click types into them all). An inject with "direct" runs the
// same checks again, at the moment of the write, on the exact bytes, then
// consumes the server nonce and writes the bytes: the command alone, never
// a newline, so it waits on the prompt line for the user's Enter. When
// any check is not met the answer is dialog:true and nothing is written:
// the page then opens the approve dialog, the path that was there before.

// directTypeMsg is the "direct" part of a type-check or an inject.
type directTypeMsg struct {
	// Ref is the page's id for this request, sent back with the answer.
	Ref string `json:"ref"`
	// Kind says where the command comes from: an agent proposal ("agent",
	// with the message's "agent" part), a snippet or a notebook's block.
	Kind       string `json:"kind"`
	SnippetID  string `json:"snippetId,omitempty"`
	NotebookID string `json:"notebookId,omitempty"`
}

const (
	directAgent    = "agent"
	directSnippet  = "snippet"
	directNotebook = "notebook"
)

// Why a direct type goes to the dialog, beyond the agent service's own.
const (
	reasonAltScreen  = "the pane shows a full-screen program (such as vim, less or htop), where typed bytes are keystrokes"
	reasonUnknown    = "Keel cannot check this command here"
	reasonUnreviewed = "the agent edited this notebook and you have not reviewed that edit"
)

// errFollowing refuses a direct type from a client that does not drive.
var errFollowing = errors.New("you follow this pane: take or request control to type")

// directCheck runs every check of a type without the dialog for this
// client of sess. A *service.DialogError sends it to the dialog; another
// error refuses it. With commit, an agent proposal's agent.type is logged
// (and a group run's host claimed) as the step before the write.
func (s *Server) directCheck(ctx context.Context, sess *runtime.Session, clientID uint64, command string, d *directTypeMsg, am *agentTypeMsg, commit bool) error {
	info := sess.Info()
	if clientID != info.DriverID {
		return errFollowing
	}
	if sess.AltScreen() {
		return &service.DialogError{Reason: reasonAltScreen}
	}
	switch d.Kind {
	case directAgent:
		if am == nil || s.agent == nil {
			return &service.DialogError{Reason: reasonUnknown}
		}
		return s.agent.AuthorizeDirect(service.TypeRequest{
			ConversationID: am.ConversationID,
			GroupRunID:     am.GroupRunID,
			ProposalID:     am.ProposalID,
			SessionID:      info.ID,
			Role:           clientRole(sess, clientID),
			Bytes:          command,
		}, commit)
	case directSnippet:
		// A snippet is the user's own text: one with a placeholder goes
		// to the dialog, as before, rather than being refused.
		if !service.DirectLine(command) || service.HasPlaceholder(command) {
			return &service.DialogError{Reason: service.ReasonLines}
		}
		snippets, err := s.store.ListSnippets()
		if err != nil {
			return &service.DialogError{Reason: service.ReasonBytes}
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
			if s.agent.RateCommand(ctx, info.ID, command, sn.AgentUsable) != agent.RiskReadOnly {
				return &service.DialogError{Reason: service.ReasonRisk}
			}
			return nil
		}
		return &service.DialogError{Reason: service.ReasonBytes}
	case directNotebook:
		// A placeholder is a shell glob, never the value (rule 9).
		if service.HasPlaceholder(command) {
			return service.ErrPlaceholder
		}
		if !service.DirectLine(command) {
			return &service.DialogError{Reason: service.ReasonLines}
		}
		nb, err := s.store.GetNotebook(d.NotebookID)
		if err != nil {
			return &service.DialogError{Reason: service.ReasonBytes}
		}
		// The dialog says so above the exact bytes (§6 rule 12).
		if nb.AgentUnreviewed {
			return &service.DialogError{Reason: reasonUnreviewed}
		}
		found := false
		for _, c := range nb.Cells {
			found = found || strings.Contains(c.Body, command)
		}
		if !found {
			return &service.DialogError{Reason: service.ReasonBytes}
		}
		// A private notebook does not exist for the agent (§6 rule 12):
		// its blocks are rated by the code rule alone.
		if s.agent.RateCommand(ctx, info.ID, command, !nb.Private) != agent.RiskReadOnly {
			return &service.DialogError{Reason: service.ReasonRisk}
		}
		return nil
	}
	return &service.DialogError{Reason: reasonUnknown}
}

// directAnswer fills an answer's ok, dialog, reason and message from err.
func directAnswer(out map[string]any, err error) {
	if reason, ok := service.NeedsDialog(err); ok {
		out["ok"], out["dialog"], out["reason"] = false, true, reason
		return
	}
	if err != nil {
		out["ok"], out["message"] = false, err.Error()
		return
	}
	out["ok"] = true
}

// typeCheck answers a type-check: whether this command would be typed
// without the dialog now. It writes and logs nothing.
func (s *Server) typeCheck(ctx context.Context, ws *wsConn, sess *runtime.Session, clientID uint64, command string, d *directTypeMsg, am *agentTypeMsg) {
	out := map[string]any{"type": "type-check", "ref": d.Ref}
	err := s.directCheck(ctx, sess, clientID, command, d, am, false)
	directAnswer(out, err)
	out["direct"] = err == nil
	_ = ws.writeJSON(out)
}

// typeDirect is an inject with "direct": the checks again, then the
// write of the command alone with the server nonce, and the log entry.
func (s *Server) typeDirect(ctx context.Context, ws *wsConn, sess *runtime.Session, clientID uint64, command, nonce string, d *directTypeMsg, am *agentTypeMsg) {
	out := map[string]any{"type": "type-direct", "ref": d.Ref}
	info := sess.Info()
	if err := s.directCheck(ctx, sess, clientID, command, d, am, true); err != nil {
		directAnswer(out, err)
		_ = ws.writeJSON(out)
		return
	}
	group := d.Kind == directAgent && am.GroupRunID != ""
	// Never a newline: the command waits on the prompt line.
	command = strings.TrimRight(command, "\r\n")
	mark := sess.StreamMark()
	if err := sess.InjectTicket(clientID, command, nonce); err != nil {
		if group {
			// The host goes back to proposed: nothing was written.
			s.agent.GroupTypeFailed(am.GroupRunID, am.ProposalID)
		}
		s.note(logKind(d.Kind), "a command was not typed: "+err.Error(), info.HostID, info.ID, "")
		out["ok"], out["message"] = false, "the type expired; press Type again"
		if errors.Is(err, runtime.ErrNotReady) {
			out["message"] = notReadyTypeMessage
		}
		_ = ws.writeJSON(out)
		return
	}
	switch {
	case group:
		s.agent.GroupTypeDoneAt(am.GroupRunID, am.ProposalID, mark)
	case d.Kind == directAgent:
		// agent.type (Source "direct") was logged by AuthorizeDirect.
		s.agent.TypeDoneAt(am.ConversationID, am.ProposalID, mark)
	default:
		// A snippet or a notebook block: its id and counts, never its text.
		if s.logs != nil {
			id := d.SnippetID
			if d.Kind == directNotebook {
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
	out["ok"] = true
	_ = ws.writeJSON(out)
}

func logKind(kind string) string {
	if kind == directAgent {
		return "agent"
	}
	return kind
}

func paneName(info runtime.Info) string {
	if info.Title != "" {
		return info.Title
	}
	return "a pane"
}
