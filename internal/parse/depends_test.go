package parse

import (
	"testing"

	"github.com/aries1980/lodo/internal/model"
)

func TestParseDepends(t *testing.T) {
	a := "11111111-1111-4111-8111-111111111111"
	b := "22222222-2222-4222-8222-222222222222"
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{"empty", "", nil, false},
		{"single", "((" + a + "))", []string{a}, false},
		{"two comma", "((" + a + ")), ((" + b + "))", []string{a, b}, false},
		{"spaces inside", "((  " + a + "  ))", []string{a}, false},
		{"duplicates collapse", "((" + a + ")), ((" + a + "))", []string{a}, false},
		{"bare uuid rejected", a, nil, true},
		{"typo rejected", "((typo-uuid))", nil, true},
		{"empty entry rejected", "((" + a + ")), ", nil, true},
		{"non-ref rejected", "hello", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDepends(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDepends(%q) err=%v wantErr=%v", tt.in, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseDepends(%q)=%v want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ParseDepends(%q)=%v want %v", tt.in, got, tt.want)
				}
			}
		})
	}
}

func TestParseOrder(t *testing.T) {
	if m, err := ParseOrder("sequential"); err != nil || m != model.OrderSequential {
		t.Fatalf("sequential: %v %v", m, err)
	}
	if m, err := ParseOrder("PARALLEL"); err != nil || m != model.OrderParallel {
		t.Fatalf("parallel: %v %v", m, err)
	}
	if _, err := ParseOrder("sideways"); err == nil {
		t.Fatal("sideways must fail")
	}
}
