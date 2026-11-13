package keelclient

import (
	"context"
	"net/http"
	"net/url"
)

// Group runs (docs/ui-redesign.md §17; internal/httpapi/agentgroups.go, groupvars.go): start one, read it, ask keel serve to
// make the current step's proposals for the panes this window has open, switch rolling and parallel, mark a host's step,
// stop it, and fill a proposal's variables for the approve dialog. None of these routes writes to a session
// (agentask_test.go checks the files that hold them): a proposal reaches a pane only through ApprovedInject (inject.go),
// once per ticked host, each with its own proposal and its own server nonce.

func groupRunPath(id string) string { return "/api/agent/groupruns/" + url.PathEscape(id) }

// StartGroupRun is POST /api/agent/groupruns {runbookId, groupId, mode}: keel serve fixes the run's hosts per step at that
// moment (the members with the step's role whose Agent can read is on). No model is called and no pane is read.
func (c *Client) StartGroupRun(ctx context.Context, runbookID, groupID, mode string) (out GroupRun, err error) {
	err = c.do(ctx, http.MethodPost, "/api/agent/groupruns", map[string]string{"runbookId": runbookID, "groupId": groupID, "mode": mode}, &out)
	return out, err
}

// GroupRun is GET /api/agent/groupruns/{id}: keel serve first reads the results of typed steps from their panes' shell marks.
func (c *Client) GroupRun(ctx context.Context, id string) (out GroupRun, err error) {
	err = c.do(ctx, http.MethodGet, groupRunPath(id), nil, &out)
	return out, err
}

// StopGroupRun is DELETE /api/agent/groupruns/{id}: its live proposals can no longer be typed. Call it only after the user
// confirmed.
func (c *Client) StopGroupRun(ctx context.Context, id string) (out GroupRun, err error) {
	err = c.do(ctx, http.MethodDelete, groupRunPath(id), nil, &out)
	return out, err
}

// SetGroupRunMode is PUT /api/agent/groupruns/{id}/mode {mode}: keel serve refuses it (409) while a host of the current step
// is proposed, being typed or waiting for its result, or after a failure stopped the step.
func (c *Client) SetGroupRunMode(ctx context.Context, id, mode string) (out GroupRun, err error) {
	err = c.do(ctx, http.MethodPut, groupRunPath(id)+"/mode", map[string]string{"mode": mode}, &out)
	return out, err
}

// ProposeGroupStep is POST /api/agent/groupruns/{id}/propose {panes}: the session ids this window has open and drives. keel
// serve resolves each pane's host itself and makes the current step's due proposals, one per host; the call is idempotent.
func (c *Client) ProposeGroupStep(ctx context.Context, id string, panes []string) (out GroupRun, err error) {
	if panes == nil {
		panes = []string{}
	}
	err = c.do(ctx, http.MethodPost, groupRunPath(id)+"/propose", map[string][]string{"panes": panes}, &out)
	return out, err
}

// FillGroupStep is POST /api/agent/groupruns/{id}/fill {proposalIds}: for the approve dialog, each live proposal with its
// variables filled in for its host, now. It writes nothing. The bytes it returns are the only place a value leaves keel
// serve; the dialog shows them and Type sends exactly them, and keel serve fills again at Type and refuses other bytes.
func (c *Client) FillGroupStep(ctx context.Context, id string, proposalIDs []string) ([]GroupFill, error) {
	var out struct {
		Fills []GroupFill `json:"fills"`
	}
	err := c.do(ctx, http.MethodPost, groupRunPath(id)+"/fill", map[string][]string{"proposalIds": proposalIDs}, &out)
	return out.Fills, err
}

// MarkGroupStep is POST /api/agent/groupruns/{id}/steps/{sid} {hostId, action}: retry, skip, done or confirm on a host of
// the current step; with no host, done for a step with no command and skip for the whole step. keel serve refuses any
// other step (409), so nothing skips ahead.
func (c *Client) MarkGroupStep(ctx context.Context, id, stepID, hostID, action string) (out GroupRun, err error) {
	err = c.do(ctx, http.MethodPost, groupRunPath(id)+"/steps/"+url.PathEscape(stepID), map[string]string{"hostId": hostID, "action": action}, &out)
	return out, err
}
