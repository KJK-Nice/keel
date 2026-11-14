package native

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
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

// The fonts' licences sit beside them.
func TestFontLicencesShip(t *testing.T) {
	for _, name := range []string{"OFL-jetbrains-mono.txt", "OFL-ibm-plex-mono.txt"} {
		if b, _ := os.ReadFile(filepath.Join("fonts", name)); len(b) == 0 {
			t.Errorf("%s is missing", name)
		}
	}
}

// Each shipped family, once registered as run.go does, renders differently from a missing family and from the other.
func TestShippedFamiliesRenderDifferently(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("registerFonts registers nothing on Linux (run.go; docs/native.md, Linux)")
	}
	t.Cleanup(func() {
		if t.Failed() { // issue #140: the widths in the default, the bundled and a missing family (linuxdiag_test.go)
			diagFonts(t)
			t.Logf("linux-diag: renders equal: bundled JetBrains vs missing %v, Plex vs missing %v, JetBrains vs Plex %v",
				bytes.Equal(renderIn(monoFamily), renderIn("keel-test-missing-5d1e")), bytes.Equal(renderIn(plexFamily), renderIn("keel-test-missing-5d1e")), bytes.Equal(renderIn(monoFamily), renderIn(plexFamily)))
		}
	})
	registerFonts()
	missing, jb, plex := renderIn("keel-test-missing-5d1e"), renderIn(monoFamily), renderIn(plexFamily)
	if bytes.Equal(jb, missing) {
		t.Error("JetBrains Mono renders as a missing family")
	}
	if bytes.Equal(plex, missing) {
		t.Error("IBM Plex Mono renders as a missing family")
	}
	if bytes.Equal(jb, plex) {
		t.Error("the two families render alike")
	}
	// The stacks the terminal is given start with the family the option names.
	for _, f := range termFonts {
		if f.stack == "" || f.label == "" || f.desc == "" {
			t.Errorf("option %+v is incomplete", f)
		}
	}
	if !bytes.Equal(renderIn(termStack(fontPlex)), plex) || !bytes.Equal(renderIn(termStack(fontJetBrains)), jb) {
		t.Error("a stack does not draw in its first family")
	}
}
