package i18n

import (
	"embed"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

//go:embed locales/*.po
var localeFS embed.FS

const (
	LangEnUS = "en-US"
	LangEnGB = "en-GB"
	Default  = LangEnUS
)

var (
	current = Default
	tables  = map[string]map[string]string{}
)

func init() {
	for _, lang := range []string{LangEnUS, LangEnGB} {
		data, err := localeFS.ReadFile("locales/" + lang + ".po")
		if err != nil {
			continue
		}
		tables[lang] = parsePO(string(data))
	}
}

func Normalize(tag string) string {
	t := strings.TrimSpace(tag)
	if t == "" {
		return ""
	}
	if i := strings.Index(t, "."); i >= 0 {
		t = t[:i]
	}
	if i := strings.Index(t, "@"); i >= 0 {
		t = t[:i]
	}
	t = strings.ReplaceAll(t, "_", "-")
	switch strings.ToLower(t) {
	case "en-gb":
		return LangEnGB
	case "en-us":
		return LangEnUS
	case "en":
		return Default
	default:
		return ""
	}
}

func SetLang(tag string) {
	if n := Normalize(tag); n != "" {
		current = n
	}
}

func Current() string {
	if current == "" {
		return Default
	}
	return current
}

func ResolveLang(flagVal string) string {
	if n := Normalize(flagVal); n != "" {
		return n
	}
	if v := strings.TrimSpace(os.Getenv("LODO_LANG")); v != "" {
		if n := Normalize(v); n != "" {
			return n
		}
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			if n := Normalize(v); n != "" {
				return n
			}
		}
	}
	return Default
}

func T(msgid string) string {
	if m, ok := tables[current]; ok {
		if s, ok := m[msgid]; ok && s != "" {
			return s
		}
	}
	return msgid
}

func Tf(msgid string, args ...any) string {
	return fmt.Sprintf(T(msgid), args...)
}

func N(singular, plural string, n int) string {
	msgid := singular
	if n != 1 {
		msgid = plural
	}
	got := T(msgid)
	if !strings.Contains(got, "%") {
		return got
	}
	return fmt.Sprintf(got, n)
}

func parsePO(content string) map[string]string {
	out := map[string]string{}
	var curID, curStr strings.Builder
	var inID, inStr bool
	flush := func() {
		if inID || inStr {
			if k := curID.String(); k != "" {
				out[k] = curStr.String()
			}
		}
		curID.Reset()
		curStr.Reset()
		inID = false
		inStr = false
	}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "msgid ") {
			if inID || inStr {
				flush()
			}
			inID = true
			inStr = false
			curID.WriteString(unquote(strings.TrimSpace(strings.TrimPrefix(line, "msgid "))))
			continue
		}
		if strings.HasPrefix(line, "msgstr ") {
			inStr = true
			inID = false
			curStr.WriteString(unquote(strings.TrimSpace(strings.TrimPrefix(line, "msgstr "))))
			continue
		}
		if strings.HasPrefix(line, "\"") {
			if inStr {
				curStr.WriteString(unquote(line))
			} else if inID {
				curID.WriteString(unquote(line))
			}
		}
	}
	flush()
	return out
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && strings.HasPrefix(s, "\"") {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
		inner := s[1 : len(s)-1]
		inner = strings.ReplaceAll(inner, "\\\"", "\"")
		inner = strings.ReplaceAll(inner, "\\n", "\n")
		return inner
	}
	return s
}

func DefaultDateFormat(lang string) string {
	if Normalize(lang) == LangEnGB {
		return "ddd dd MMM yyyy"
	}
	return "ddd MMM dd yyyy"
}

func DefaultTimeFormat(_ string) string {
	return "HH:mm"
}

func ToGoLayout(format string, isDate bool) (string, error) {
	f := strings.TrimSpace(format)
	if f == "" {
		return "", fmt.Errorf("empty format")
	}
	var b strings.Builder
	i := 0
	matched := false
	for i < len(f) {
		rest := f[i:]
		if len(rest) >= 4 && strings.EqualFold(rest[:4], "dddd") {
			b.WriteString("Monday")
			i += 4
			matched = true
			continue
		}
		if len(rest) >= 4 && strings.EqualFold(rest[:4], "mmmm") {
			b.WriteString("January")
			i += 4
			matched = true
			continue
		}
		if len(rest) >= 4 && strings.EqualFold(rest[:4], "yyyy") {
			b.WriteString("2006")
			i += 4
			matched = true
			continue
		}
		if len(rest) >= 3 && strings.EqualFold(rest[:3], "ddd") {
			b.WriteString("Mon")
			i += 3
			matched = true
			continue
		}
		if len(rest) >= 3 && strings.EqualFold(rest[:3], "mmm") {
			b.WriteString("Jan")
			i += 3
			matched = true
			continue
		}
		if len(rest) >= 2 && strings.EqualFold(rest[:2], "hh") {
			if isDate {
				return "", fmt.Errorf("format %q: hour token not allowed in date format", format)
			}
			switch rest[:2] {
			case "HH":
				b.WriteString("15")
			case "hh":
				b.WriteString("03")
			default:
				b.WriteString("15")
			}
			i += 2
			matched = true
			continue
		}
		if len(rest) >= 2 && strings.EqualFold(rest[:2], "dd") {
			b.WriteString("02")
			i += 2
			matched = true
			continue
		}
		if len(rest) >= 2 && strings.EqualFold(rest[:2], "mm") {
			if isDate {
				b.WriteString("01")
			} else {
				b.WriteString("04")
			}
			i += 2
			matched = true
			continue
		}
		if len(rest) >= 2 && strings.EqualFold(rest[:2], "yy") {
			b.WriteString("06")
			i += 2
			matched = true
			continue
		}
		if len(rest) >= 2 && strings.EqualFold(rest[:2], "ss") {
			if isDate {
				return "", fmt.Errorf("format %q: time token not allowed in date format", format)
			}
			b.WriteString("05")
			i += 2
			matched = true
			continue
		}
		c := rest[0]
		lc := strings.ToLower(string(c))[0]
		switch {
		case lc == 'd' && isDate:
			b.WriteString("2")
			i++
			matched = true
		case lc == 'm' && isDate:
			b.WriteString("1")
			i++
			matched = true
		case lc == 'm' && !isDate:
			b.WriteString("4")
			i++
			matched = true
		case lc == 'y' && isDate:
			return "", fmt.Errorf("format %q: use yyyy or yy for year", format)
		case lc == 'd' && !isDate:
			return "", fmt.Errorf("format %q: day token not allowed in time format", format)
		case lc == 'y' && !isDate:
			return "", fmt.Errorf("format %q: year token not allowed in time format", format)
		case c == 'H' && !isDate:
			b.WriteString("15")
			i++
			matched = true
		case c == 'h' && !isDate:
			b.WriteString("3")
			i++
			matched = true
		case lc == 'h' && !isDate:
			b.WriteString("15")
			i++
			matched = true
		case lc == 's' && !isDate:
			b.WriteString("5")
			i++
			matched = true
		case (c == 'a' || c == 'A') && !isDate:
			b.WriteString("PM")
			i++
			matched = true
		case c == '/' || c == '-' || c == '.' || c == ',' || c == ':' || c == ' ' || c == '\'' || c == '(' || c == ')' || c == '[' || c == ']':
			b.WriteByte(c)
			i++
		default:
			return "", fmt.Errorf("format %q: unsupported token near %q (date: dddd ddd dd d MMMM MMM MM M yyyy yy; time: HH H hh h mm m ss s a)", format, rest)
		}
	}
	if !matched {
		return "", fmt.Errorf("format %q: no date/time tokens found", format)
	}
	return b.String(), nil
}

func FormatDate(t time.Time, customFormat string) string {
	lang := Current()
	f := strings.TrimSpace(customFormat)
	if f == "" {
		f = DefaultDateFormat(lang)
	}
	layout, err := ToGoLayout(f, true)
	if err != nil {
		if lang == LangEnGB {
			return t.Format("Mon 02 Jan 2006")
		}
		return t.Format("Mon Jan 02 2006")
	}
	return t.Format(layout)
}

func FormatTime(t time.Time, customFormat string) string {
	lang := Current()
	f := strings.TrimSpace(customFormat)
	if f == "" {
		f = DefaultTimeFormat(lang)
	}
	layout, err := ToGoLayout(f, false)
	if err != nil {
		return t.Format("15:04")
	}
	s := t.Format(layout)
	if strings.Contains(layout, "PM") && strings.Contains(f, "a") && !strings.Contains(f, "A") {
		s = strings.ReplaceAll(s, "PM", "pm")
		s = strings.ReplaceAll(s, "AM", "am")
	}
	return s
}

func ValidateDateFormat(f string) error {
	_, err := ToGoLayout(f, true)
	return err
}

func ValidateTimeFormat(f string) error {
	_, err := ToGoLayout(f, false)
	return err
}
