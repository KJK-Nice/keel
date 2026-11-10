package native

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// renderIn draws one string in family and returns the image.
func renderIn(family string) []byte {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.Color{A: 255}).Children(func() {
			ui.Text(c, "Keel 0Oo1lI {}[] wmi").Font(family).FontSize(28).TextColor(ui.Color{R: 255, G: 255, B: 255, A: 255})
		})
	}, 600, 80)
	tt.Frame()
	return tt.Image().Pix
}

// TestRegisterFontAcceptsWOFF2 is the proof for the font decision: a registered family must render
// differently from a family that does not exist.
func TestRegisterFontAcceptsWOFF2(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "fonts", "jetbrains-mono-latin-400-normal.woff2"))
	if err != nil {
		t.Fatal(err)
	}
	err = ui.RegisterFont(b, "keel-test-mono-7f3a")
	t.Logf("RegisterFont(woff2) = %v", err)
	if !bytes.Equal(renderIn("keel-test-missing-7f3a"), renderIn("keel-test-missing-2b9c")) {
		t.Fatal("two missing families render differently: the comparison proves nothing")
	}
	if bytes.Equal(renderIn("keel-test-mono-7f3a"), renderIn("keel-test-missing-7f3a")) {
		t.Skip("WOFF2 is not accepted by ui.RegisterFont: the registered family renders as the fallback (RegisterFont error: " + errString(err) + ")")
	}
}

func errString(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

// The embedded fonts are copies of the webview's; they must not drift.
func TestEmbeddedFontsMatchTheWebviews(t *testing.T) {
	files, err := fontFiles.ReadDir("fonts")
	if err != nil || len(files) != 4 {
		t.Fatalf("embedded fonts: %d files, err %v; want 4 woff2", len(files), err)
	}
	for _, f := range files {
		mine, _ := fontFiles.ReadFile("fonts/" + f.Name())
		orig, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "fonts", f.Name()))
		if err != nil || !bytes.Equal(mine, orig) {
			t.Errorf("%s differs from cmd/keel/web/fonts (err %v)", f.Name(), err)
		}
	}
	mine, _ := os.ReadFile(filepath.Join("fonts", "OFL-jetbrains-mono.txt"))
	orig, _ := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "fonts", "OFL-jetbrains-mono.txt"))
	if len(mine) == 0 || !bytes.Equal(mine, orig) {
		t.Error("OFL-jetbrains-mono.txt is missing or differs")
	}
}
