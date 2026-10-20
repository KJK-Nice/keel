//go:build ignore

// Generate Keel's macOS app icon (PNG + ICNS, plus Assets.car when actool
// exists) from the Icon Composer document Keel.icon.
//
//	go run genicon.go
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
)

// Apple's 1024pt macOS icon grid: squircle is 824pt, not full-bleed.
// Flattened Default renditions fill the canvas; Dock then looks oversized
// next to live Liquid Glass icons. Inset the ICNS/PNG fallback.
const iconGrid = 824.0 / 1024.0

func main() {
	dir, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	iconDoc := filepath.Join(dir, "Keel.icon")
	if _, err := os.Stat(iconDoc); err != nil {
		fatal(fmt.Errorf("Keel.icon: %w", err))
	}
	ictool, err := lookICTool()
	if err != nil {
		fatal(err)
	}

	sizes := []int{16, 32, 64, 128, 256, 512, 1024}
	encoded := make(map[int][]byte, len(sizes))
	for _, s := range sizes {
		inner := int(math.Round(float64(s) * iconGrid))
		if inner < 1 {
			inner = 1
		}
		tmp, err := os.CreateTemp("", fmt.Sprintf("keel-icon-%d-*.png", s))
		if err != nil {
			fatal(err)
		}
		tmpPath := tmp.Name()
		_ = tmp.Close()
		cmd := exec.Command(ictool, iconDoc,
			"--export-image",
			"--output-file", tmpPath,
			"--platform", "macOS",
			"--rendition", "Default",
			"--width", fmt.Sprintf("%d", inner),
			"--height", fmt.Sprintf("%d", inner),
			"--scale", "1",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			_ = os.Remove(tmpPath)
			fatal(fmt.Errorf("ictool %d: %w: %s", s, err, bytes.TrimSpace(out)))
		}
		data, err := os.ReadFile(tmpPath)
		_ = os.Remove(tmpPath)
		if err != nil {
			fatal(err)
		}
		padded, err := padPNG(data, s)
		if err != nil {
			fatal(fmt.Errorf("pad %d: %w", s, err))
		}
		encoded[s] = padded
	}

	if err := os.WriteFile(filepath.Join(dir, "icon.png"), encoded[1024], 0o644); err != nil {
		fatal(err)
	}

	// Pack with iconutil. It stores the 1x 16 and 32 px sizes as ARGB
	// (ic04/ic05): macOS reads icp4/icp5 as raw pixels, so PNG data there
	// shows as noise on non-Retina displays.
	if err := packICNS(dir, encoded); err != nil {
		fatal(err)
	}

	if err := compileAssetCatalog(dir, iconDoc); err != nil {
		fmt.Fprintf(os.Stderr, "Assets.car skipped: %v\n", err)
	}
	fmt.Println("wrote icon.png and Keel.icns from Keel.icon")
}

func padPNG(src []byte, size int) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	ib := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	x := (size - ib.Dx()) / 2
	y := (size - ib.Dy()) / 2
	draw.Draw(dst, image.Rect(x, y, x+ib.Dx(), y+ib.Dy()), img, ib.Min, draw.Over)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compileAssetCatalog(dir, iconDoc string) error {
	actool, err := lookACTool()
	if err != nil {
		return err
	}
	outDir, err := os.MkdirTemp("", "keel-actool-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(outDir)
	plist := filepath.Join(outDir, "partial.plist")
	cmd := exec.Command(actool, iconDoc,
		"--compile", outDir,
		"--output-format", "human-readable-text",
		"--notices", "--warnings", "--errors",
		"--output-partial-info-plist", plist,
		"--app-icon", "Keel",
		"--include-all-app-icons",
		"--enable-on-demand-resources", "NO",
		"--development-region", "en",
		"--target-device", "mac",
		"--minimum-deployment-target", "26.0",
		"--platform", "macosx",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("actool: %w: %s", err, bytes.TrimSpace(out))
	}
	car := filepath.Join(outDir, "Assets.car")
	data, err := os.ReadFile(car)
	if err != nil {
		return fmt.Errorf("actool wrote no Assets.car: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Assets.car"), data, 0o644); err != nil {
		return err
	}
	fmt.Println("wrote Assets.car from Keel.icon")
	return nil
}

func lookICTool() (string, error) {
	// Prefer Icon Composer.app. /usr/bin/ictool is an xcode-select stub
	// that fails when only Command Line Tools are installed.
	const bundled = "/Applications/Icon Composer.app/Contents/Executables/ictool"
	if _, err := os.Stat(bundled); err == nil {
		return bundled, nil
	}
	if p, err := exec.LookPath("ictool"); err == nil && p != "/usr/bin/ictool" {
		return p, nil
	}
	return "", fmt.Errorf("ictool not found; install Icon Composer")
}

func lookACTool() (string, error) {
	candidates := []string{}
	if p, err := exec.LookPath("actool"); err == nil {
		candidates = append(candidates, p)
	}
	if out, err := exec.Command("xcrun", "--find", "actool").Output(); err == nil {
		if p := string(bytes.TrimSpace(out)); p != "" {
			candidates = append(candidates, p)
		}
	}
	candidates = append(candidates, "/Applications/Xcode.app/Contents/Developer/usr/bin/actool")
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		out, err := exec.Command(p, "--version").CombinedOutput()
		if err == nil && !bytes.Contains(out, []byte("requires Xcode")) {
			return p, nil
		}
	}
	return "", fmt.Errorf("actool not found; install Xcode to compile Liquid Glass Assets.car")
}

// iconset maps each .iconset file name to the pixel size it holds.
var iconset = map[string]int{
	"icon_16x16.png":      16,
	"icon_16x16@2x.png":   32,
	"icon_32x32.png":      32,
	"icon_32x32@2x.png":   64,
	"icon_128x128.png":    128,
	"icon_128x128@2x.png": 256,
	"icon_256x256.png":    256,
	"icon_256x256@2x.png": 512,
	"icon_512x512.png":    512,
	"icon_512x512@2x.png": 1024,
}

func packICNS(dir string, encoded map[int][]byte) error {
	tmp, err := os.MkdirTemp("", "keel-icns-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	set := filepath.Join(tmp, "Keel.iconset")
	if err := os.Mkdir(set, 0o755); err != nil {
		return err
	}
	for name, size := range iconset {
		if err := os.WriteFile(filepath.Join(set, name), encoded[size], 0o644); err != nil {
			return err
		}
	}
	out, err := exec.Command("iconutil", "-c", "icns", set, "-o", filepath.Join(dir, "Keel.icns")).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iconutil: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
