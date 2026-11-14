//go:build !keelfake

package app

import (
	"fmt"

	"keel/internal/agent/service"
)

// agentProviders returns how keel serve builds the model provider and the
// detector, and whether they are the test fakes. This build has only the
// real clients: the fakes are linked with the tag keelfake (agent_fake.go),
// and asking for them here stops keel serve, so a test never runs against
// a real provider by mistake.
func agentProviders(choice string) (service.Factory, service.JudgeFactory, bool, error) {
	if choice == "fake" {
		return nil, nil, false, fmt.Errorf("%s=fake needs a test build of keel (go build -tags keelfake; make native-e2e builds one): this build has no test provider", agentProviderEnv)
	}
	return service.Real, service.RealJudge, false, nil
}
