//go:build keelfake

package app

import (
	"keel/internal/agent/provider"
	"keel/internal/agent/provider/providertest"
	"keel/internal/agent/service"
)

// Test builds only (make native-e2e, make native-automation-build). Only
// the exact value "fake" changes anything: it selects providertest.Scripted
// and providertest.ScriptedJudge, which answer in this process and have no
// network code, so this switch cannot send pane text anywhere. Any other
// value (or none) gives the real clients.
func agentProviders(choice string) (service.Factory, service.JudgeFactory, bool, error) {
	if choice == "fake" {
		fake := providertest.NewScripted()
		judge := providertest.NewScriptedJudge()
		return func(string, string) (provider.Provider, error) { return fake, nil },
			func(string) (provider.Judge, error) { return judge, nil }, true, nil
	}
	return service.Real, service.RealJudge, false, nil
}
