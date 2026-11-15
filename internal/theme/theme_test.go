package theme_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aries1980/lodo/internal/theme"
)

func TestForMonokaiHasTruecolor(t *testing.T) {
	th := theme.For(theme.Monokai)
	if th.Concrete {
		t.Fatal("monokai must not be concrete")
	}
	if !strings.HasPrefix(th.Green, "38;2;") {
		t.Fatalf("monokai green = %q, want truecolor SGR", th.Green)
	}
	if th.Palette.Todo == "" || th.Palette.Title == "" {
		t.Fatalf("monokai palette incomplete: %+v", th.Palette)
	}
}

func TestForConcreteIsPlain(t *testing.T) {
	for _, name := range []string{"", "concrete", "CONCRETE", "  "} {
		th := theme.For(name)
		if !th.Concrete {
			t.Fatalf("For(%q) must be concrete", name)
		}
	}
}

func TestParseBytesMonokai(t *testing.T) {
	raw, err := os.ReadFile("themes/monokai.yaml")
	if err != nil {
		t.Skip("shipped monokai.yaml not present")
	}
	th, err := theme.ParseBytes(raw)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if th.Concrete {
		t.Fatal("monokai.yaml must not parse as concrete")
	}
	if !strings.HasPrefix(th.Red, "38;2;") {
		t.Fatalf("red = %q", th.Red)
	}
}

func TestAllShippedThemesParse(t *testing.T) {
	entries, err := os.ReadDir("themes")
	if err != nil {
		t.Fatalf("ReadDir themes: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no shipped themes")
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".yaml")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("themes", e.Name()))
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			th, err := theme.ParseBytes(raw)
			if err != nil {
				t.Fatalf("ParseBytes: %v", err)
			}
			if th.Name != name {
				t.Fatalf("theme name = %q, want file base %q", th.Name, name)
			}
			if got := theme.For(name); got.Concrete == th.Concrete && got.Name != th.Name {
				t.Fatalf("For(%q).Name = %q, want %q", name, got.Name, th.Name)
			}
			if name == theme.Concrete {
				if !th.Concrete {
					t.Fatal("concrete.yaml must parse as concrete")
				}
				return
			}
			if th.Concrete {
				t.Fatal("must not parse as concrete")
			}
			if th.Red == "" || th.Green == "" || th.Yellow == "" || th.Cyan == "" {
				t.Fatalf("cli roles incomplete: %+v", th)
			}
			if th.Palette.Title == "" || th.Palette.Err == "" || th.Palette.TableSelBg == "" {
				t.Fatalf("tui roles incomplete: %+v", th.Palette)
			}
		})
	}
}

func TestParseBytesRejectsBadHex(t *testing.T) {
	_, err := theme.ParseBytes([]byte("name: bad\ncli:\n  red: \"#ZZZZZZ\"\n"))
	if err == nil {
		t.Fatal("bad hex must fail")
	}
}

func TestResolveUnknownFallsBackConcrete(t *testing.T) {
	th, warn := theme.Resolve("no-such-theme", t.TempDir())
	if !th.Concrete {
		t.Fatal("unknown theme must fall back to concrete")
	}
	if !strings.Contains(warn, "falling back to concrete") {
		t.Fatalf("warn = %q", warn)
	}
}

func TestResolveCustomPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "custom.yaml")
	body := "name: custom\ncli:\n  green: \"#00FF00\"\ntui:\n  ok: \"#00FF00\"\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	th, warn := theme.Resolve(p, dir)
	if warn != "" {
		t.Fatalf("warn = %q", warn)
	}
	if th.Concrete {
		t.Fatal("custom file must not be concrete")
	}
	if th.Green != "38;2;0;255;0" {
		t.Fatalf("green = %q", th.Green)
	}
	if th.Palette.Ok != "#00FF00" {
		t.Fatalf("ok = %q", th.Palette.Ok)
	}
}

func TestResolveMissingPathFallsBack(t *testing.T) {
	th, warn := theme.Resolve(filepath.Join(t.TempDir(), "absent.yaml"), t.TempDir())
	if !th.Concrete {
		t.Fatal("missing file must fall back to concrete")
	}
	if !strings.Contains(warn, "falling back to concrete") {
		t.Fatalf("warn = %q", warn)
	}
}

func TestResolveThemeByName(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "lodo", "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: acme\ncli:\n  green: \"#00FF00\"\ntui:\n  ok: \"#00FF00\"\n"
	if err := os.WriteFile(filepath.Join(dir, "acme.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", cfg)

	th, warn := theme.Resolve("acme", cfg)
	if warn != "" {
		t.Fatalf("warn = %q", warn)
	}
	if th.Name != "acme" {
		t.Fatalf("name = %q, want acme", th.Name)
	}
	if th.Palette.Ok != "#00FF00" {
		t.Fatalf("ok = %q", th.Palette.Ok)
	}

	th, warn = theme.Resolve("..", cfg)
	if !th.Concrete {
		t.Fatal("a bare .. must not resolve")
	}
	if !strings.Contains(warn, "unknown theme") {
		t.Fatalf("warn = %q", warn)
	}
}
