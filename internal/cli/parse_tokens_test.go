package cli

import (
	"reflect"
	"testing"
)

func TestParseGraphTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want map[string]string
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty", in: []string{}, want: nil},
		{name: "valid", in: []string{"work=abc", "family=def"}, want: map[string]string{"work": "abc", "family": "def"}},
		{name: "trims", in: []string{" work = abc "}, want: map[string]string{"work": "abc"}},
		{name: "value may contain equals", in: []string{"work=a=b=c"}, want: map[string]string{"work": "a=b=c"}},
		{name: "ignores malformed", in: []string{"oops", "work=abc"}, want: map[string]string{"work": "abc"}},
		{name: "ignores empty name or token", in: []string{"=abc", "work=", " = ", "ok=1"}, want: map[string]string{"ok": "1"}},
		{name: "all ignored becomes nil", in: []string{"oops", "=x"}, want: nil},
		{name: "last wins for duplicate name", in: []string{"work=a", "work=b"}, want: map[string]string{"work": "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseGraphTokens(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseGraphTokens(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}
