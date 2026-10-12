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
