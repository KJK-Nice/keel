package paths

import (
	"os"
	"path/filepath"
)

const DefaultListen = "127.0.0.1:47241"

func Addr() string {
	if v := os.Getenv("KEEL_ADDR"); v != "" {
		return v
	}
	return DefaultListen
}

func PidFile(dataDir string) string {
	return filepath.Join(dataDir, "keel-serve.pid")
}

// TokenFile holds the secret that authenticates clients of keel serve.
func TokenFile(dataDir string) string {
	return filepath.Join(dataDir, "keel-serve.token")
}

func DataDir() string {
	if v := os.Getenv("KEEL_DATA"); v != "" {
		return v
	}
	dir, err := os.UserConfigDir()
	if err == nil && dir != "" {
		return filepath.Join(dir, "keel")
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, ".keel")
	}
	return "data"
}

// SSHDir is the folder whose config the first-run import reads: ~/.ssh,
// or KEEL_SSH_DIR (tests point it at a fixture so they never read the real
// one). ~ in that config stands for the folder above it.
func SSHDir() string {
	if v := os.Getenv("KEEL_SSH_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh")
}
