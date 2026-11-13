package service

import (
	"keel/internal/agent/provider"
	"keel/internal/agent/provider/anthropic"
	"keel/internal/agent/provider/gemini"
	"keel/internal/agent/provider/jev"
)

// Factory builds the provider for one Ask from the provider id in the
// settings and the key read from the vault.
type Factory func(providerID, key string) (provider.Provider, error)

// Real builds the real client for a provider. This package is the only one
// allowed to import provider/gemini and provider/anthropic
// (internal/agent/invariants_test.go), and Ask is the only caller, so
// Google and Anthropic are reached only on a user's Ask.
func Real(providerID, key string) (provider.Provider, error) {
	switch providerID {
	case "gemini":
		return gemini.New(key)
	case "anthropic":
		return anthropic.New(key)
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
