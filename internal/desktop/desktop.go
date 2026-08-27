package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func Run(url, profileDir string) error {
	bin, err := LookPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return err
	}
	cmd := exec.Command(bin, chromeArgs(url, profileDir)...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start desktop window: %w", err)
	}
	return cmd.Wait()
}

func chromeArgs(url, profileDir string) []string {
	return []string{
		"--app=" + url,
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--window-size=1280,840",
	}
}

func LookPath() (string, error) {
	if v := strings.TrimSpace(os.Getenv("KEEL_BROWSER")); v != "" {
		if path, err := exec.LookPath(v); err == nil {
			return path, nil
		}
		if _, err := os.Stat(v); err == nil {
			return v, nil
		}
		return "", fmt.Errorf("KEEL_BROWSER=%s not found", v)
	}
	for _, c := range candidates() {
		if filepath.IsAbs(c) {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
			continue
		}
		if path, err := exec.LookPath(c); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no desktop browser found (install Chrome, Chromium, Edge, or Brave, or set KEEL_BROWSER)")
}

func candidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"google-chrome",
			"chromium",
		}
	case "windows":
		return []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
			"chrome",
			"msedge",
		}
	default:
		return []string{
			"/opt/google/chrome/chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium-browser",
			"/usr/bin/chromium",
			"google-chrome-stable",
			"google-chrome",
			"chromium",
			"chromium-browser",
			"microsoft-edge",
			"microsoft-edge-stable",
			"brave-browser",
			"brave",
		}
	}
}
