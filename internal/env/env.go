// Package env reads Slime's environment variables. Each is SLIME_<name>; the name from before the rename,
// KEEL_<name>, still works when the SLIME_ one is unset or empty, so scripts and launch agents written for Keel keep
// working.
package env

import "os"

// Get returns SLIME_<name>, else KEEL_<name>, else "".
func Get(name string) string {
	if v := os.Getenv("SLIME_" + name); v != "" {
		return v
	}
	return os.Getenv("KEEL_" + name)
}

// Names are both spellings of name, for code that must strip or unset the variable whichever one is set.
func Names(name string) []string { return []string{"SLIME_" + name, "KEEL_" + name} }
