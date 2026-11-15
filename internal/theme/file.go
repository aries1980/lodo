package theme

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed themes/*.yaml
var shippedFS embed.FS

type fileCLI struct {
	Bold   string `yaml:"bold"`
	Dim    string `yaml:"dim"`
	Red    string `yaml:"red"`
	Green  string `yaml:"green"`
	Yellow string `yaml:"yellow"`
	Cyan   string `yaml:"cyan"`
}

type fileTUI struct {
	PropKey     string `yaml:"propKey"`
	PropSep     string `yaml:"propSep"`
	Todo        string `yaml:"todo"`
	Doing       string `yaml:"doing"`
	Done        string `yaml:"done"`
	Code        string `yaml:"code"`
	Link        string `yaml:"link"`
	Ref         string `yaml:"ref"`
	Head        string `yaml:"head"`
	Hint        string `yaml:"hint"`
	Title       string `yaml:"title"`
	Dim         string `yaml:"dim"`
	Err         string `yaml:"err"`
	Ok          string `yaml:"ok"`
	Select      string `yaml:"select"`
	TableBorder string `yaml:"tableBorder"`
	TableSelFg  string `yaml:"tableSelFg"`
	TableSelBg  string `yaml:"tableSelBg"`
}

type fileTheme struct {
	Name string  `yaml:"name"`
	CLI  fileCLI `yaml:"cli"`
	TUI  fileTUI `yaml:"tui"`
}

func ParseBytes(data []byte) (Theme, error) {
	var ft fileTheme
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&ft); err != nil {
		return Theme{}, fmt.Errorf("theme file: %w", err)
	}
	name := Normalize(ft.Name)
	if name == "" {
		name = Concrete
	}
	th := Theme{Name: name}
	if name == Concrete && ft.CLI.Red == "" && ft.TUI.PropKey == "" {
		th.Concrete = true
		return th, nil
	}
	var err error
	if th.Bold, err = cliSpecToSGR(ft.CLI.Bold, true); err != nil {
		return Theme{}, fmt.Errorf("theme file cli.bold: %w", err)
	}
	if th.Dim, err = cliSpecToSGR(ft.CLI.Dim, true); err != nil {
		return Theme{}, fmt.Errorf("theme file cli.dim: %w", err)
	}
	if th.Red, err = cliSpecToSGR(ft.CLI.Red, false); err != nil {
		return Theme{}, fmt.Errorf("theme file cli.red: %w", err)
	}
	if th.Green, err = cliSpecToSGR(ft.CLI.Green, false); err != nil {
		return Theme{}, fmt.Errorf("theme file cli.green: %w", err)
	}
	if th.Yellow, err = cliSpecToSGR(ft.CLI.Yellow, false); err != nil {
		return Theme{}, fmt.Errorf("theme file cli.yellow: %w", err)
	}
	if th.Cyan, err = cliSpecToSGR(ft.CLI.Cyan, false); err != nil {
		return Theme{}, fmt.Errorf("theme file cli.cyan: %w", err)
	}
	p := Palette{}
	if p.PropKey, err = tuiSpecToLipgloss(ft.TUI.PropKey); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.propKey: %w", err)
	}
	if p.PropSep, err = tuiSpecToLipgloss(ft.TUI.PropSep); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.propSep: %w", err)
	}
	if p.Todo, err = tuiSpecToLipgloss(ft.TUI.Todo); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.todo: %w", err)
	}
	if p.Doing, err = tuiSpecToLipgloss(ft.TUI.Doing); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.doing: %w", err)
	}
	if p.Done, err = tuiSpecToLipgloss(ft.TUI.Done); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.done: %w", err)
	}
	if p.Code, err = tuiSpecToLipgloss(ft.TUI.Code); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.code: %w", err)
	}
	if p.Link, err = tuiSpecToLipgloss(ft.TUI.Link); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.link: %w", err)
	}
	if p.Ref, err = tuiSpecToLipgloss(ft.TUI.Ref); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.ref: %w", err)
	}
	if p.Head, err = tuiSpecToLipgloss(ft.TUI.Head); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.head: %w", err)
	}
	if p.Hint, err = tuiSpecToLipgloss(ft.TUI.Hint); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.hint: %w", err)
	}
	if p.Title, err = tuiSpecToLipgloss(ft.TUI.Title); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.title: %w", err)
	}
	if p.Dim, err = tuiSpecToLipgloss(ft.TUI.Dim); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.dim: %w", err)
	}
	if p.Err, err = tuiSpecToLipgloss(ft.TUI.Err); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.err: %w", err)
	}
	if p.Ok, err = tuiSpecToLipgloss(ft.TUI.Ok); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.ok: %w", err)
	}
	if p.Select, err = tuiSpecToLipgloss(ft.TUI.Select); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.select: %w", err)
	}
	if p.TableBorder, err = tuiSpecToLipgloss(ft.TUI.TableBorder); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.tableBorder: %w", err)
	}
	if p.TableSelFg, err = tuiSpecToLipgloss(ft.TUI.TableSelFg); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.tableSelFg: %w", err)
	}
	if p.TableSelBg, err = tuiSpecToLipgloss(ft.TUI.TableSelBg); err != nil {
		return Theme{}, fmt.Errorf("theme file tui.tableSelBg: %w", err)
	}
	th.Palette = p
	if th.Bold == "" && th.Dim == "" && th.Red == "" && th.Green == "" && th.Yellow == "" && th.Cyan == "" && p == (Palette{}) {
		th.Concrete = true
		if th.Name == "" {
			th.Name = Concrete
		}
	}
	return th, nil
}

func ParseFile(path string) (Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, err
	}
	th, err := ParseBytes(data)
	if err != nil {
		return Theme{}, fmt.Errorf("%s: %w", path, err)
	}
	return th, nil
}

func parseRootFile(root *os.Root, name string) (Theme, error) {
	data, err := root.ReadFile(name)
	if err != nil {
		return Theme{}, err
	}
	th, err := ParseBytes(data)
	if err != nil {
		return Theme{}, fmt.Errorf("%s: %w", filepath.Join(root.Name(), name), err)
	}
	return th, nil
}

func cliSpecToSGR(spec string, isAttr bool) (string, error) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return "", nil
	}
	if strings.HasPrefix(s, "#") {
		r, g, b, err := hexToRGB(s)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("38;2;%d;%d;%d", r, g, b), nil
	}
	if isAttr {
		if s == "1" || s == "2" {
			return s, nil
		}
		if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 255 {
			return fmt.Sprintf("38;5;%d", n), nil
		}
		return "", fmt.Errorf("%q: want \"1\", \"2\", a 0-255 color, or #RRGGBB", spec)
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 255 {
		return fmt.Sprintf("38;5;%d", n), nil
	}
	if isRawSGR(s) {
		return s, nil
	}
	return "", fmt.Errorf("%q: want #RRGGBB, a 0-255 color, or an SGR code", spec)
}

func isRawSGR(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && c != ';' {
			return false
		}
	}
	return strings.Contains(s, ";")
}

func tuiSpecToLipgloss(spec string) (string, error) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return "", nil
	}
	if strings.HasPrefix(s, "#") {
		if _, _, _, err := hexToRGB(s); err != nil {
			return "", err
		}
		return strings.ToUpper(s), nil
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 255 {
		return strconv.Itoa(n), nil
	}
	return "", fmt.Errorf("%q: want #RRGGBB or a 0-255 color", spec)
}

func hexToRGB(s string) (int, int, int, error) {
	h := strings.TrimPrefix(s, "#")
	if len(h) != 6 {
		return 0, 0, 0, fmt.Errorf("%q: want #RRGGBB", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%q: want #RRGGBB", s)
	}
	return int(v >> 16 & 0xFF), int(v >> 8 & 0xFF), int(v & 0xFF), nil
}

func shipped(name string) (Theme, bool) {
	raw, err := shippedFS.ReadFile("themes/" + Normalize(name) + ".yaml")
	if err != nil {
		return Theme{}, false
	}
	th, err := ParseBytes(raw)
	if err != nil {
		return Theme{}, false
	}
	return th, true
}

func looksLikePath(v string) bool {
	t := strings.TrimSpace(v)
	if t == "" {
		return false
	}
	if strings.HasPrefix(t, "~") {
		return true
	}
	if strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../") || strings.HasPrefix(t, "/") {
		return true
	}
	if strings.HasSuffix(strings.ToLower(t), ".yaml") || strings.HasSuffix(strings.ToLower(t), ".yml") {
		return true
	}
	return strings.Contains(t, "/") || strings.Contains(t, "\\")
}

func expandPath(p string, configDir string) string {
	if rest, ok := strings.CutPrefix(p, "~"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			rest, _ = strings.CutPrefix(rest, "/")
			return filepath.Join(home, rest)
		}
	}
	if filepath.IsAbs(p) {
		return p
	}
	if configDir != "" {
		return filepath.Join(configDir, p)
	}
	return p
}

func Resolve(value, configDir string) (Theme, string) {
	v := strings.TrimSpace(value)
	if v == "" || Normalize(v) == Concrete {
		return For(Concrete), ""
	}
	if !looksLikePath(v) {
		name := Normalize(v)
		if th, ok := shipped(name); ok {
			return th, ""
		}
		for _, dir := range themeDirs() {
			root, err := os.OpenRoot(dir)
			if err != nil {
				continue
			}
			th, err := parseRootFile(root, name+".yaml")
			_ = root.Close()
			if err == nil {
				th.Name = name
				return th, ""
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return For(Concrete), fmt.Sprintf("ui.theme %q: %s; falling back to concrete", v, strings.TrimSpace(err.Error()))
			}
		}
		return For(Concrete), fmt.Sprintf("ui.theme %q: unknown theme; falling back to concrete", v)
	}
	p := expandPath(v, configDir)
	th, err := ParseFile(p)
	if err != nil {
		return For(Concrete), fmt.Sprintf("ui.theme %q: %s; falling back to concrete", v, strings.TrimSpace(err.Error()))
	}
	if th.Name == "" || th.Name == Concrete {
		th.Name = strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	}
	return th, ""
}

func themeDirs() []string {
	var out []string
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		out = append(out, filepath.Join(xdg, "lodo", "themes"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".config", "lodo", "themes"))
	}
	out = append(out, "/usr/share/lodo/themes")
	return out
}
