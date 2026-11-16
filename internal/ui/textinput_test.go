package ui

import "testing"

func TestTextFieldEditCaretAndWindow(t *testing.T) {
	var f textField
	if !f.Insert("daft punk") {
		t.Fatal("Insert returned false")
	}
	if f.Text() != "daft punk" || f.Caret() != 9 {
		t.Fatalf("after Insert: text=%q caret=%d", f.Text(), f.Caret())
	}

	// Move left 4 runes -> before "punk" (index 5).
	for i := 0; i < 4; i++ {
		f.MoveLeft()
	}
	if f.Caret() != 5 {
		t.Fatalf("caret after 4 MoveLeft = %d, want 5", f.Caret())
	}

	f.Insert("& ")
	if f.Text() != "daft & punk" || f.Caret() != 7 {
		t.Fatalf("after mid-insert: text=%q caret=%d", f.Text(), f.Caret())
	}

	if !f.Backspace() || f.Text() != "daft &punk" || f.Caret() != 6 {
		t.Fatalf("after Backspace: text=%q caret=%d", f.Text(), f.Caret())
	}
	if !f.Delete() || f.Text() != "daft &unk" || f.Caret() != 6 {
		t.Fatalf("after Delete: text=%q caret=%d", f.Text(), f.Caret())
	}

	f.Home()
	if f.Caret() != 0 {
		t.Fatalf("caret after Home = %d, want 0", f.Caret())
	}
	f.MoveRight()
	if f.Caret() != 1 {
		t.Fatalf("caret after MoveRight = %d, want 1", f.Caret())
	}
	f.End()
	if f.Caret() != 9 {
		t.Fatalf("caret after End = %d, want 9", f.Caret())
	}

	// Unicode + horizontal windowing around caret.
	f.SetText("0123456789abcdefghijklmnopqrstuvwxyz")
	vis, col, ps, pe := f.DisplayWindow(10)
	if vis != "qrstuvwxyz" || col != 10 || ps != -1 || pe != -1 {
		t.Fatalf("DisplayWindow at end = (%q, %d, %d, %d)", vis, col, ps, pe)
	}

	// Move caret to middle and check preedit splicing.
	f.SetText("hello world")
	for i := 0; i < 6; i++ {
		f.MoveLeft()
	}
	f.preedit = "漢字"
	vis, col, ps, pe = f.DisplayWindow(20)
	if vis != "hello漢字 world" || col != 7 || ps != 5 || pe != 7 {
		t.Fatalf("DisplayWindow with preedit = (%q, %d, %d, %d)", vis, col, ps, pe)
	}
}
