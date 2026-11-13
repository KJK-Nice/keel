package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/store"
)

// The one place anything leaves the machine (docs/ui-redesign.md §6 rules
// 3, 4 and 7). A request to a model provider or to the judge (Jev) goes
// through send and nowhere else (internal/agent/invariants_test.go):
//
//   - it can hold only redacted text: provider.Request and
//     provider.JudgeRequest take redact.Safe, which only a Redactor (or a
//     constant) makes;
//   - send writes the agent.read entry itself, before the call, and does
//     not call when the entry cannot be written. A send without a log
//     entry cannot be written down in this package.

// jevName is the judge's name in keel.log.
const jevName = "TypeSafe Jev"

// line is a provider client built for one request, with the key it was
// built with, which send removes from any error.
type line struct {
	model provider.Provider
	judge provider.Judge
	key   string
}

// outbound is one request: a model's or the judge's, and its log entry.
type outbound struct {
	model *provider.Request
	judge *provider.JudgeRequest
	log   agent.Egress
	// perWindow: prompt lines sent to notice plain language (§7) go on
	// every typing pause, so the first of a window is logged at once and
	// the rest in one entry when the window ends (noteDetect).
	perWindow bool
}

// reply is the answer to an outbound: model for a model request, judge
// for a judge request.
type reply struct {
	model provider.Response
	judge provider.JudgeResponse
}

// send logs out and sends it to the provider on the line. An error from
// the provider comes back as a *ProviderError with the key removed.
func (s *Service) send(ctx context.Context, to line, out outbound) (reply, error) {
	if (out.model == nil) == (out.judge == nil) || (out.model != nil && to.model == nil) || (out.judge != nil && to.judge == nil) {
		return reply{}, errors.New("agent: a request needs one provider of its kind")
	}
	var err error
	if out.perWindow {
		err = s.noteDetect(out.log)
	} else {
		err = s.audit.Sent(out.log)
	}
	if err != nil {
		return reply{}, err
	}
	var r reply
	if out.model != nil {
		r.model, err = to.model.Generate(ctx, *out.model)
	} else {
		r.judge, err = to.judge.Judge(ctx, *out.judge)
	}
	if err != nil {
		return r, &ProviderError{Msg: scrub(err.Error(), to.key)}
	}
	return r, nil
}

// providerFor reads the model provider's key from the vault and builds the
// client for one request. It and judgeFor are the only places in the agent
// tree that read a key (internal/agent/invariants_test.go).
func (s *Service) providerFor(id string) (line, error) {
	key, err := s.st.AgentKey(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return line{}, ErrNoKey
		}
		return line{}, err
	}
	p, err := s.factory(id, key)
	if err != nil {
		if errors.Is(err, provider.ErrNoKey) {
			return line{}, ErrNoKey
		}
		return line{}, &ProviderError{Msg: scrub(err.Error(), key)}
	}
	return line{model: p, key: key}, nil
}

// judgeFor reads the detector key (Jev) from the vault and builds its
// client for one request. ErrNoKey means only that no key is set: the
// caller falls back to #-lines and the code rating. Any other error (the
// vault cannot be read, the client cannot be built) is returned as it is,
// so a caller that must fail closed can tell the two apart.
func (s *Service) judgeFor() (line, error) {
	configured, err := s.st.AgentKeyConfigured(JudgeProvider)
	if err != nil {
		return line{}, err
	}
	if !configured {
		return line{}, ErrNoKey
	}
	key, err := s.st.AgentKey(JudgeProvider)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return line{}, ErrNoKey
		}
		return line{}, err
	}
	j, err := s.judge(key)
	if err != nil {
		if errors.Is(err, provider.ErrNoKey) {
			return line{}, ErrNoKey
		}
		return line{}, &ProviderError{Msg: scrub(err.Error(), key)}
	}
	return line{judge: j, key: key}, nil
}

func scrub(msg, key string) string {
	if key != "" {
		msg = strings.ReplaceAll(msg, key, "[key]")
	}
	return msg
}

func (s *Service) providerErr(parent, cctx context.Context, err error) error {
	switch {
	case errors.Is(err, agent.ErrAuditLog):
		return err // nothing was sent
	case parent.Err() != nil:
		return context.Canceled
	case errors.Is(cctx.Err(), context.DeadlineExceeded):
		return &ProviderError{Msg: fmt.Sprintf("the provider did not answer within %s", s.timeout)}
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe
	}
	return &ProviderError{Msg: err.Error()}
}
