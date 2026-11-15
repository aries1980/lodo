package theme

import (
	"slices"
	"strings"
)

const (
	Concrete = "concrete"
	Monokai  = "monokai"
)

func Valid(name string) bool {
	n := Normalize(name)
	if n == Concrete {
		return true
	}
	_, ok := shipped(n)
	return ok
}

func Normalize(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return Concrete
	}
	return n
}

func Names() []string {
	entries, err := shippedFS.ReadDir("themes")
	if err != nil {
		return []string{Concrete, Monokai}
	}
	names := []string{Concrete}
	var rest []string
	for _, e := range entries {
		n := strings.TrimSuffix(e.Name(), ".yaml")
		if n == "" || n == Concrete {
			continue
		}
		rest = append(rest, n)
	}
	slices.Sort(rest)
	return append(names, rest...)
}

type Palette struct {
	PropKey     string
	PropSep     string
	Todo        string
	Doing       string
	Done        string
	Code        string
	Link        string
	Ref         string
	Head        string
	Hint        string
	Title       string
	Dim         string
	Err         string
	Ok          string
	Select      string
	TableBorder string
	TableSelFg  string
	TableSelBg  string
}

type Theme struct {
	Name     string
	Concrete bool
	Bold     string
	Dim      string
	Red      string
	Green    string
	Yellow   string
	Cyan     string
	Palette  Palette
}

func For(name string) Theme {
	n := Normalize(name)
	if n == Concrete {
		return Theme{Name: Concrete, Concrete: true}
	}
	if th, ok := shipped(n); ok {
		return th
	}
	if n == Monokai {
		return Theme{
			Name:   Monokai,
			Bold:   "1",
			Dim:    "2",
			Red:    "38;2;249;38;114",
			Green:  "38;2;166;226;46",
			Yellow: "38;2;230;219;116",
			Cyan:   "38;2;102;217;239",
			Palette: Palette{
				PropKey:     "#66D9EF",
				PropSep:     "#75715E",
				Todo:        "#E6DB74",
				Doing:       "#A6E22E",
				Done:        "#75715E",
				Code:        "#AE81FF",
				Link:        "#66D9EF",
				Ref:         "#AE81FF",
				Head:        "#F8F8F2",
				Hint:        "#75715E",
				Title:       "#F8F8F2",
				Dim:         "#75715E",
				Err:         "#F92672",
				Ok:          "#A6E22E",
				Select:      "#E6DB74",
				TableBorder: "#75715E",
				TableSelFg:  "#F8F8F2",
				TableSelBg:  "#49483E",
			},
		}
	}
	return Theme{Name: Concrete, Concrete: true}
}
