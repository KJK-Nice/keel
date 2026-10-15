package service

import (
	"keel/internal/agent/provider"
	"keel/internal/agent/provider/gemini"
	"keel/internal/agent/provider/jev"
	"keel/internal/agent/provider/providertest"
)

// Factory builds the provider for one Ask from the provider id in the
// settings and the key read from the vault.
type Factory func(providerID, key string) (provider.Provider, error)

// Real builds the real client for a provider. This package is the only one
// allowed to import provider/gemini (internal/agent/invariants_test.go),
// and Ask is the only caller, so Google is reached only on a user's Ask.
func Real(providerID, key string) (provider.Provider, error) {
	switch providerID {
	case "gemini":
		return gemini.New(key)
	}
	return nil, provider.ErrUnknown
}

// JudgeFactory builds the detector for in-pane suggestions from the key
// read from the vault.
type JudgeFactory func(key string) (provider.Judge, error)

// JudgeProvider is the provider id of the detector's key in the vault.
const JudgeProvider = "jev"

// RealJudge builds the TypeSafe Jev client. This package is the only one
// allowed to import provider/jev (internal/agent/invariants_test.go), and
// judgeFor is the only caller, so TypeSafe is reached only from a pane's
// prompt line or a suggestion the user asked for.
func RealJudge(key string) (provider.Judge, error) {
	return jev.New(key)
}

// TestProviderEnv names the environment variable that swaps every provider
// for the in-process test fake. It exists for end-to-end tests only.
const TestProviderEnv = "KEEL_AGENT_PROVIDER"

// FromEnv returns the factories keel serve uses (model provider and
// detector), and whether they are the test fakes. Only the exact value
// "fake" changes anything: it selects providertest.Scripted and
// providertest.ScriptedJudge, which answer in this process and have no
// network code, so this switch cannot send pane text anywhere. Any other
// value (or none) gives the real clients.
func FromEnv(getenv func(string) string) (Factory, JudgeFactory, bool) {
	if getenv(TestProviderEnv) == "fake" {
		fake := providertest.NewScripted()
		judge := providertest.NewScriptedJudge()
		return func(string, string) (provider.Provider, error) { return fake, nil },
			func(string) (provider.Judge, error) { return judge, nil }, true
	}
	return Real, RealJudge, false
}
