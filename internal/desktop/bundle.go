package desktop

import (
	"path/filepath"
	"strings"
)

const bundleEnv = "KEEL_MAC_BUNDLE"

func looksLikeAppBundle(exe string) bool {
	slash := filepath.ToSlash(exe)
	return strings.Contains(slash, ".app/Contents/MacOS/")
}

func withBundleEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	prefix := bundleEnv + "="
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return append(out, bundleEnv+"=1")
}
