//go:build ignore

// Generate Keel's macOS app icon (PNG + ICNS) from the Icon Composer
// document Keel.icon via ictool (Icon Composer.app).
//
//	go run genicon.go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

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
			"--width", fmt.Sprintf("%d", s),
			"--height", fmt.Sprintf("%d", s),
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
		if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
			fatal(fmt.Errorf("ictool %d did not write a PNG", s))
		}
		encoded[s] = data
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
	fmt.Println("wrote icon.png and Keel.icns from Keel.icon")
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
