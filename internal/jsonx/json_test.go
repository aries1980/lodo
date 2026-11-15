package jsonx

import "testing"

func TestMarshal(t *testing.T) {
	in := struct {
		Values map[string]string `json:"values"`
		Slice  []string          `json:"slice"`
		Map    map[string]string `json:"map"`
	}{Values: map[string]string{"z": "<>&", "a": "first"}}
	got, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"values":{"a":"first","z":"\u003c\u003e\u0026"},"slice":null,"map":null}`
	if string(got) != want {
		t.Fatalf("Marshal = %s, want %s", got, want)
	}
}

func TestMarshalEmptyCollections(t *testing.T) {
	in := struct {
		Slice []string          `json:"slice"`
		Map   map[string]string `json:"map"`
	}{Slice: []string{}, Map: map[string]string{}}
	got, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"slice":[],"map":{}}`; string(got) != want {
		t.Fatalf("Marshal = %s, want %s", got, want)
	}
}

func TestMarshalIndent(t *testing.T) {
	got, err := MarshalIndent(map[string]int{"b": 2, "a": 1}, "  ")
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"a\": 1,\n  \"b\": 2\n}"; string(got) != want {
		t.Fatalf("MarshalIndent = %s, want %s", got, want)
	}
}

func TestUnmarshal(t *testing.T) {
	var out struct {
		Value string `json:"value"`
	}
	if err := Unmarshal([]byte(`{"VALUE":"first","value":"second","value":"last"}`), &out); err != nil {
		t.Fatal(err)
	}
	if out.Value != "last" {
		t.Fatalf("Value = %q, want last", out.Value)
	}
}

func TestErrors(t *testing.T) {
	if _, err := Marshal(make(chan int)); err == nil {
		t.Fatal("Marshal must reject unsupported values")
	}
	if _, err := MarshalIndent(make(chan int), "  "); err == nil {
		t.Fatal("MarshalIndent must reject unsupported values")
	}
	var out any
	if err := Unmarshal([]byte(`{"broken":`), &out); err == nil {
		t.Fatal("Unmarshal must reject malformed JSON")
	}
}
