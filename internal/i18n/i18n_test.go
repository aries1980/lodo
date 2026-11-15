package i18n_test

import (
	"testing"

	"github.com/aries1980/lodo/internal/i18n"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"gb dash", "en-GB", i18n.LangEnGB},
		{"gb underscore", "en_GB", i18n.LangEnGB},
		{"gb encoding", "en-GB.UTF-8", i18n.LangEnGB},
		{"gb modifier", "en_GB@euro", i18n.LangEnGB},
		{"gb lower", "en-gb", i18n.LangEnGB},
		{"gb upper", "EN-GB", i18n.LangEnGB},
		{"us dash", "en-US", i18n.LangEnUS},
		{"us underscore encoding", "en_US.UTF-8", i18n.LangEnUS},
		{"bare en defaults", "en", i18n.Default},
		{"bare EN defaults", "EN", i18n.Default},
		{"empty", "", ""},
		{"unrelated", "fr-FR", ""},
		{"C locale", "C", ""},
		{"C UTF-8", "C.UTF-8", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := i18n.Normalize(tc.in); got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveLangPrecedence(t *testing.T) {
	cases := []struct {
		name       string
		flag       string
		lodoLang   string
		lcAll      string
		lcMessages string
		lang       string
		want       string
	}{
		{"flag wins over env", "en-GB", "en-US", "en-US", "en-US", "en-US", i18n.LangEnGB},
		{"flag encoding", "en_GB.UTF-8", "", "", "", "", i18n.LangEnGB},
		{"invalid flag falls to LODO_LANG", "fr-FR", "en-GB", "en-US", "en-US", "en-US", i18n.LangEnGB},
		{"LODO_LANG wins over LC_*", "", "en-GB", "en-US", "en-US", "en-US", i18n.LangEnGB},
		{"invalid LODO_LANG falls to LC_ALL", "", "fr-FR", "en-GB", "en-US", "en-US", i18n.LangEnGB},
		{"LC_ALL wins over LC_MESSAGES", "", "", "en-GB", "en-US", "en-US", i18n.LangEnGB},
		{"LC_MESSAGES wins over LANG", "", "", "", "en-GB", "en-US", i18n.LangEnGB},
		{"LANG last", "", "", "", "", "en-GB", i18n.LangEnGB},
		{"LANG encoding", "", "", "", "", "en_GB.UTF-8", i18n.LangEnGB},
		{"bare en defaults", "", "en", "", "", "", i18n.Default},
		{"all empty defaults", "", "", "", "", "", i18n.Default},
		{"all invalid defaults", "fr-FR", "fr-FR", "fr-FR", "fr-FR", "fr-FR", i18n.Default},
		{"C locale ignored", "", "", "", "", "C.UTF-8", i18n.Default},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LODO_LANG", tc.lodoLang)
			t.Setenv("LC_ALL", tc.lcAll)
			t.Setenv("LC_MESSAGES", tc.lcMessages)
			t.Setenv("LANG", tc.lang)
			if got := i18n.ResolveLang(tc.flag); got != tc.want {
				t.Fatalf("ResolveLang(flag=%q) = %q, want %q (LODO_LANG=%q LC_ALL=%q LC_MESSAGES=%q LANG=%q)",
					tc.flag, got, tc.want, tc.lodoLang, tc.lcAll, tc.lcMessages, tc.lang)
			}
		})
	}
}
