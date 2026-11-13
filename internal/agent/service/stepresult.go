package service

import (
	"fmt"

	"keel/internal/shellmark"
)

// shellReport is what a pane's shell reported for a typed step.
type shellReport struct {
	exit      int
	noStatus  bool // a command ended with no exit status
	abandoned bool // and its shell went away
}

// cmdResult reads a step's result from the commands a shell reported after
// its mark: any non-zero exit fails it at once; it is done once want
// commands ended, each with exit 0. ok is false while it waits.
func cmdResult(cmds []shellmark.Command, want int) (rep shellReport, ok bool) {
	ended := 0
	for _, cmd := range cmds {
		if !cmd.Done {
			break
		}
		ended++
		switch {
		case cmd.Abandoned || !cmd.HasExit:
			return shellReport{noStatus: true, abandoned: cmd.Abandoned}, true
		case cmd.Exit != 0:
			return shellReport{exit: cmd.Exit}, true
		}
		if ended >= want {
			return shellReport{}, true
		}
	}
	return shellReport{}, false
}

// stepOutcome is what a shell's report makes of a typed step. A runbook's
// step and a group run's cell record the same decision.
type stepOutcome struct {
	result   string // ResultReported, ResultUnreported or ResultAbandoned
	exit     *int   // nil when the shell gave no status
	finished bool   // false: the step waits for the user
	failed   bool   // finished with a non-zero exit
	confirm  bool   // exit 0 on a production host: the user confirms it
	log      string // the log line's ending, after the step's name
}

// outcome decides a typed step's result. Marks are the remote's own output
// and prove nothing, so a result is labelled reported; no status leaves the
// step to the user, and on a production host so does a reported exit 0.
func (r shellReport) outcome(production bool) stepOutcome {
	exit := r.exit
	switch {
	case r.noStatus:
		o := stepOutcome{result: ResultUnreported, log: " ended with no exit status reported"}
		if r.abandoned {
			o.result = ResultAbandoned
		}
		return o
	case exit == 0 && production:
		return stepOutcome{result: ResultReported, exit: &exit, confirm: true, log: " · exit 0 reported by the shell · waiting for you to confirm (production host)"}
	}
	return stepOutcome{result: ResultReported, exit: &exit, finished: true, failed: exit != 0, log: fmt.Sprintf(" · exit %d reported by the shell", exit)}
}
