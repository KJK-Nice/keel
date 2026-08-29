//go:build ignore

// Generate Keel's macOS app icon (PNG + ICNS, plus Assets.car when actool
// exists) from the Icon Composer document Keel.icon.
//
//	go run genicon.go
package main

import (
	"bytes"
	"encoding/binary"
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

	// PNG-based ICNS elements. Largest first; @2x types reuse the 2× raster.
	chunks := []icnsChunk{
		{typ: "ic10", data: encoded[1024]},
		{typ: "ic09", data: encoded[512]},
		{typ: "ic14", data: encoded[512]},
		{typ: "ic08", data: encoded[256]},
		{typ: "ic13", data: encoded[256]},
		{typ: "ic07", data: encoded[128]},
		{typ: "icp6", data: encoded[64]},
		{typ: "ic12", data: encoded[64]},
		{typ: "icp5", data: encoded[32]},
		{typ: "ic11", data: encoded[32]},
		{typ: "icp4", data: encoded[16]},
	}
	if err := os.WriteFile(filepath.Join(dir, "Keel.icns"), packICNS(chunks), 0o644); err != nil {
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

type icnsChunk struct {
	typ  string
	data []byte
}

func packICNS(chunks []icnsChunk) []byte {
	var body bytes.Buffer
	for _, c := range chunks {
		if len(c.typ) != 4 {
			fatal(fmt.Errorf("icns type %q must be 4 bytes", c.typ))
		}
		body.WriteString(c.typ)
		if err := binary.Write(&body, binary.BigEndian, uint32(8+len(c.data))); err != nil {
			fatal(err)
		}
		body.Write(c.data)
	}
	out := make([]byte, 8+body.Len())
	copy(out[0:4], "icns")
	binary.BigEndian.PutUint32(out[4:8], uint32(len(out)))
	copy(out[8:], body.Bytes())
	return out
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
