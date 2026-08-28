package desktop

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"testing"
)

func TestAppIconPNG(t *testing.T) {
	cfg, err := png.DecodeConfig(bytes.NewReader(appIcon))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 1024 || cfg.Height != 1024 {
		t.Fatalf("icon.png is %dx%d, want 1024x1024", cfg.Width, cfg.Height)
	}
	img, err := png.Decode(bytes.NewReader(appIcon))
	if err != nil {
		t.Fatal(err)
	}
	// Flattened Icon Composer Default: squircle with transparent corners.
	_, _, _, a := img.At(0, 0).RGBA()
	if a>>8 != 0 {
		t.Fatalf("corner alpha=%02x, want transparent", a>>8)
	}
	_, _, _, a = img.At(512, 0).RGBA()
	if a>>8 != 0xff {
		t.Fatalf("top-edge alpha=%02x, want opaque squircle", a>>8)
	}
	r, g, b, a := img.At(512, 160).RGBA()
	if r>>8 < 0xd0 || g>>8 < 0xd0 || b>>8 < 0xd0 || a>>8 != 0xff {
		t.Fatalf("chevron peak = #%02x%02x%02x, want near-white", r>>8, g>>8, b>>8)
	}
}

func TestMacOSICNS(t *testing.T) {
	data, err := os.ReadFile("macos/Keel.icns")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8 || string(data[:4]) != "icns" {
		t.Fatalf("Keel.icns missing icns magic: %q", data[:min(4, len(data))])
	}
	if n := binary.BigEndian.Uint32(data[4:8]); int(n) != len(data) {
		t.Fatalf("icns length %d, file %d", n, len(data))
	}
	want := map[string]bool{
		"ic10": false, "ic09": false, "ic08": false, "ic07": false,
		"icp6": false, "icp5": false, "icp4": false,
		"ic14": false, "ic13": false, "ic12": false, "ic11": false,
	}
	for i := 8; i < len(data); {
		if i+8 > len(data) {
			t.Fatal("truncated icns chunk header")
		}
		typ := string(data[i : i+4])
		n := int(binary.BigEndian.Uint32(data[i+4 : i+8]))
		if n < 8 || i+n > len(data) {
			t.Fatalf("bad icns chunk %q length %d at %d", typ, n, i)
		}
		payload := data[i+8 : i+n]
		if len(payload) < 8 || string(payload[:8]) != "\x89PNG\r\n\x1a\n" {
			t.Fatalf("icns %q is not a PNG", typ)
		}
		if _, ok := want[typ]; ok {
			want[typ] = true
		}
		i += n
	}
	for typ, seen := range want {
		if !seen {
			t.Errorf("Keel.icns missing %s", typ)
		}
	}
}
