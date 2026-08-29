//go:build darwin

package desktop

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

//go:embed macos/Info.plist
var macInfoPlist []byte

//go:embed macos/entitlements.plist
var macEntitlements []byte

//go:embed macos/Keel.icns
var macAppIcon []byte

//go:embed all:macos/Keel.icon
var macIconComposer embed.FS

// EnsureMacApp re-execs inside a signed .app so WKWebView can run.
// A naked Go binary on Apple Silicon hits SIGTRAP in WebKit's JIT.
func EnsureMacApp() error {
	if os.Getenv(bundleEnv) == "1" {
		return nil
	}
	src, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(src); err == nil {
		src = resolved
	}
	if looksLikeAppBundle(src) {
		return nil
	}
	if _, err := exec.LookPath("codesign"); err != nil {
		return fmt.Errorf("codesign not found; install Xcode Command Line Tools (xcode-select --install)")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("cache dir: %w", err)
	}
	appDir := filepath.Join(cache, "keel", "Keel.app")
	macOSDir := filepath.Join(appDir, "Contents", "MacOS")
	if err := os.MkdirAll(macOSDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(macOSDir, "keel")
	if err := copyFile(src, dest); err != nil {
		return fmt.Errorf("copy into app bundle: %w", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "Contents", "Info.plist"), macInfoPlist, 0o644); err != nil {
		return err
	}
	resDir := filepath.Join(appDir, "Contents", "Resources")
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(resDir, "Keel.icns"), macAppIcon, 0o644); err != nil {
		return err
	}
	if err := writeIconComposer(resDir); err != nil {
		return err
	}
	ent := filepath.Join(appDir, "Contents", "keel.entitlements")
	if err := os.WriteFile(ent, macEntitlements, 0o644); err != nil {
		return err
	}
	// Ad-hoc sign, no hardened runtime. --options runtime without a
	// Developer ID is what turns WebKit JIT into SIGTRAP.
	if err := codesign(ent, dest); err != nil {
		return err
	}
	if err := codesign(ent, appDir); err != nil {
		return err
	}

	log.Printf("relaunching as %s (WebKit needs an app bundle)", appDir)
	argv := append([]string{dest}, os.Args[1:]...)
	return syscall.Exec(dest, argv, withBundleEnv(os.Environ()))
}

func codesign(ent, target string) error {
	cmd := exec.Command("codesign", "--force", "--sign", "-", "--identifier", "app.keel.desktop", "--entitlements", ent, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign %s: %w: %s", target, err, bytes.TrimSpace(out))
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	_ = os.Remove(tmp)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func writeIconComposer(resDir string) error {
	const root = "macos/Keel.icon"
	return fs.WalkDir(macIconComposer, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(resDir, "Keel.icon", rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := macIconComposer.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
}
