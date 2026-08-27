package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddrDefaultIsLoopback(t *testing.T) {
	t.Setenv("KEEL_ADDR", "")
	_ = os.Unsetenv("KEEL_ADDR")
	if Addr() != DefaultListen {
		t.Fatalf("Addr() = %q", Addr())
	}
}

func TestAddrOverride(t *testing.T) {
	t.Setenv("KEEL_ADDR", "0.0.0.0:9")
	if Addr() != "0.0.0.0:9" {
		t.Fatalf("Addr() = %q", Addr())
	}
}

func TestDataDirOverride(t *testing.T) {
	t.Setenv("KEEL_DATA", "/tmp/keel-test-data")
	if DataDir() != "/tmp/keel-test-data" {
		t.Fatalf("DataDir() = %q", DataDir())
	}
}

func TestDataDirUsesConfig(t *testing.T) {
	t.Setenv("KEEL_DATA", "")
	_ = os.Unsetenv("KEEL_DATA")
	got := DataDir()
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Skip(err)
	}
	want := filepath.Join(cfg, "keel")
	if got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
}
