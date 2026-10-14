package service

import (
	"keel/internal/agent/provider"
	"keel/internal/agent/provider/gemini"
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

// TestProviderEnv names the environment variable that swaps every provider
// for the in-process test fake. It exists for end-to-end tests only.
const TestProviderEnv = "KEEL_AGENT_PROVIDER"

// FromEnv returns the factory keel serve uses, and whether it is the test
// fake. Only the exact value "fake" changes anything: it selects
// providertest.Scripted, which builds replies in this process and has no
// network code, so this switch cannot send pane text anywhere. Any other
// value (or none) gives the real clients.
func FromEnv(getenv func(string) string) (Factory, bool) {
	if getenv(TestProviderEnv) == "fake" {
		fake := providertest.NewScripted()
		return func(string, string) (provider.Provider, error) { return fake, nil }, true
	}
	return Real, false
}
