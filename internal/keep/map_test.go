package keep

import (
	"testing"
)

func TestOutlineRoundTrip(t *testing.T) {
	text := "Deep Work\ndefault:: true\n  Mon-Fri 09:00-11:00\n  Sat 10:00-12:00 yellow\nErrands\n  Sat 10:00-13:00"
	tree := ParseTextNote("notes/1", text)
	if len(tree) != 2 {
		t.Fatalf("maps: %+v", tree)
	}
	if len(tree[0].Children) != 2 {
		t.Fatalf("windows: %+v", tree[0].Children)
	}
	if tree[0].PropsText["default"] != "true" {
		t.Fatalf("props: %v", tree[0].PropsText)
	}
	back := RenderTextNote(tree)
	again := ParseTextNote("notes/1", back)
	if len(again) != 2 || len(again[0].Children) != 2 || again[0].PropsText["default"] != "true" {
		t.Fatalf("round trip: %q", back)
	}
}

func TestItemMapping(t *testing.T) {
	b := ItemToBlock("notes/1", Item{ID: "notes/1/listItems/1", Text: "TODO Draft\nduration:: 90m", Checked: false})
	if b.Marker != "TODO" {
		t.Fatalf("marker: %q", b.Marker)
	}
	if b.PropsText["duration"] != "90m" {
		t.Fatalf("props: %v", b.PropsText)
	}
	if b.UUID != "notes/1#notes/1/listItems/1" {
		t.Fatalf("uuid: %q", b.UUID)
	}
	done := ItemToBlock("notes/1", Item{ID: "x", Text: "old", Checked: true})
	if done.Marker != "DONE" {
		t.Fatalf("marker: %q", done.Marker)
	}
}

func TestCheckedFor(t *testing.T) {
	if !CheckedFor("DONE Write") || !CheckedFor("CANCELLED Drop") {
		t.Fatal("DONE/CANCELLED must check the item")
	}
	if CheckedFor("TODO Write") || CheckedFor("DOING Write") || CheckedFor("plain note") {
		t.Fatal("TODO/DOING/plain must not check the item")
	}
}

func TestFindByTitleDuplicates(t *testing.T) {
	notes := []Note{{ID: "a", Title: "day"}, {ID: "b", Title: " Day "}}
	if _, err := FindByTitle(notes, "day"); err == nil {
		t.Fatal("want duplicate-title error")
	}
	if _, err := FindByTitle(notes[:1], "DAY"); err != nil {
		t.Fatal(err)
	}
	if _, err := FindByTitle(notes[:1], "missing"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSelectNotes(t *testing.T) {
	notes := []Note{{ID: "a", Title: "a", Labels: []string{"labels/1"}}, {ID: "b", Title: "b"}}
	if got := SelectNotes(notes, "", ""); len(got) != 2 {
		t.Fatalf("empty filter keeps all: %+v", got)
	}
	got := SelectNotes(notes, "lodo", "labels/9")
	if len(got) != 0 {
		t.Fatalf("no match: %+v", got)
	}
	got = SelectNotes([]Note{{ID: "a", Title: "a", Labels: []string{"Lodo"}}}, "lodo", "")
	if len(got) != 1 {
		t.Fatalf("name match: %+v", got)
	}
}
