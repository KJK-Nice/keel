package native

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"keel/internal/paths"
)

// The automation guards: MyGo's automation plugin (a socket that can type into the app) is linked only with the tag
// keelautomation, no release path passes the tag, and a tagged build refuses real data.

const automationPkg = "github.com/egoist/mygo/plugins/automation"

func autoRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

// goDeps is `go list -deps` of a command with the given tags, as one string.
func goDeps(t *testing.T, pkg, tags, cgo string) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-tags="+tags, pkg)
	cmd.Dir = autoRepoRoot(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED="+cgo)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list %s: %v\n%s", pkg, err, out)
	}
	return string(out)
}

func hasPkg(deps string) bool {
	for _, l := range strings.Split(deps, "\n") {
		if strings.TrimSpace(l) == automationPkg {
			return true
		}
	}
	return false
}

func TestAutomationPluginIsLinkedOnlyWithItsTag(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	for _, c := range []struct{ pkg, tags, cgo string }{
		{"./cmd/keel", "", "0"},
		{"./cmd/keel-native", "", "0"},
	} {
		if hasPkg(goDeps(t, c.pkg, c.tags, c.cgo)) {
			t.Errorf("%s without keelautomation links the automation plugin", c.pkg)
		}
		tagged := strings.Trim(c.tags+",keelautomation", ",")
		if !hasPkg(goDeps(t, c.pkg, tagged, c.cgo)) {
			t.Errorf("%s with keelautomation does not link the automation plugin", c.pkg)
		}
	}
}

func TestAutomationIsImportedOnlyByTheTaggedFiles(t *testing.T) {
	root := autoRepoRoot(t)
	var found []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".claude", ".scratch-native", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "_ \""+automationPkg+"\"\n") && !strings.HasSuffix(p, "_test.go") {
			rel, _ := filepath.Rel(root, p)
			found = append(found, filepath.ToSlash(rel))
			if !strings.HasPrefix(string(b), "//go:build keelautomation\n") {
				t.Errorf("%s imports the automation plugin without //go:build keelautomation on its first line", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "cmd/keel/automation.go cmd/keel-native/automation.go"
	if got := strings.Join(found, " "); got != want {
		t.Errorf("the plugin is imported by %q, want exactly %q", got, want)
	}
}

func TestNoReleasePathPassesTheAutomationTag(t *testing.T) {
	root := autoRepoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	target := ""
	for _, l := range strings.Split(string(mk), "\n") {
		if l != "" && l[0] != '\t' && l[0] != ' ' && l[0] != '#' {
			if i := strings.Index(l, ":"); i > 0 {
				target = strings.TrimSpace(l[:i])
			}
		}
		if strings.Contains(l, "keelautomation") && target != "native-automation-build" {
			t.Errorf("Makefile target %q passes the tag keelautomation: %s", target, l)
		}
	}
	scripts, _ := filepath.Glob(filepath.Join(root, "scripts", "*"))
	flows, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*"))
	for _, p := range append(scripts, flows...) {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), "keelautomation") {
			t.Errorf("%s names the tag keelautomation", p)
		}
	}
}

func TestCheckAutomationEnv(t *testing.T) {
	scratch := t.TempDir()
	private := filepath.Join(t.TempDir(), "sock")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	open := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(open, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o755); err != nil {
		t.Fatal(err)
	}
	def := defaultDataDir()
	if def == "" {
		t.Fatal("no default data dir")
	}

	for _, c := range []struct {
		name, sock, data string
		ok               bool
	}{
		{"off without a socket", "", "", true},
		{"scratch data, private dir", filepath.Join(private, "a.sock"), scratch, true},
		{"no KEEL_DATA", filepath.Join(private, "a.sock"), "", false},
		{"default data dir", filepath.Join(private, "a.sock"), def, false},
		{"default data dir, other spelling", filepath.Join(private, "a.sock"), def + "/../" + filepath.Base(def), false},
		{"open socket dir", filepath.Join(open, "a.sock"), scratch, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("MYGO_AUTOMATION", c.sock)
			t.Setenv("KEEL_DATA", c.data)
			if err := CheckAutomationEnv(); (err == nil) != c.ok {
				t.Fatalf("CheckAutomationEnv = %v, want ok=%v", err, c.ok)
			}
		})
	}
}

func TestDefaultDataDirMatchesPaths(t *testing.T) {
	t.Setenv("KEEL_DATA", "")
	want := paths.DataDir()
	t.Setenv("KEEL_DATA", "/somewhere/else")
	if got := defaultDataDir(); got != want {
		t.Errorf("defaultDataDir = %q, want %q", got, want)
	}
	if os.Getenv("KEEL_DATA") != "/somewhere/else" {
		t.Error("defaultDataDir changed KEEL_DATA")
	}
}
