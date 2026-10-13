//go:build !unix

package app

// lockDataDir is a no-op where flock is unavailable (Windows): the
// listening port still keeps two daemons off the same KEEL_ADDR.
func lockDataDir(string) (func(), error) {
	return func() {}, nil
}
