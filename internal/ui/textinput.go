package ui

import (
	"image"
	"strings"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2/exp/textinput"
)

// textField is a single-line UTF-8 rune buffer with caret navigation,
// horizontal windowing, and Ebitengine IME Composer support.
type textField struct {
	text  string
	caret int

	preedit      string
	preeditStart int
	preeditEnd   int

	composer      *textinput.Composer
	sessionActive bool
	caretBounds   image.Rectangle
	changed       bool
	committedTick bool
	endedByUser   bool
}

func (f *textField) Text() string {
	return f.text
}

func (f *textField) Caret() int {
	return f.caret
}

func (f *textField) Preedit() string {
	return f.preedit
}

func (f *textField) SetText(s string) {
	clean := sanitizeSingleLine(s)
	f.text = clean
	f.caret = utf8.RuneCountInString(clean)
	f.preedit = ""
	f.preeditStart = 0
	f.preeditEnd = 0
}

func (f *textField) Clear() {
	f.SetText("")
}

func (f *textField) Insert(s string) bool {
	clean := sanitizeSingleLine(s)
	if clean == "" {
		return false
	}
	runes := []rune(f.text)
	caret := clampCaret(f.caret, len(runes))
	ins := []rune(clean)

	out := make([]rune, 0, len(runes)+len(ins))
	out = append(out, runes[:caret]...)
	out = append(out, ins...)
	out = append(out, runes[caret:]...)
	f.text = string(out)
	f.caret = caret + len(ins)
	return true
}

func (f *textField) Backspace() bool {
	if f.caret <= 0 {
		return false
	}
	runes := []rune(f.text)
	caret := clampCaret(f.caret, len(runes))
	if caret == 0 {
		return false
	}
	out := make([]rune, 0, len(runes)-1)
	out = append(out, runes[:caret-1]...)
	out = append(out, runes[caret:]...)
	f.text = string(out)
	f.caret = caret - 1
	return true
}

func (f *textField) Delete() bool {
	runes := []rune(f.text)
	caret := clampCaret(f.caret, len(runes))
	if caret >= len(runes) {
		return false
	}
	out := make([]rune, 0, len(runes)-1)
	out = append(out, runes[:caret]...)
	out = append(out, runes[caret+1:]...)
	f.text = string(out)
	return true
}

func (f *textField) MoveLeft() bool {
	if f.caret <= 0 {
		return false
	}
	f.caret--
	return true
}

func (f *textField) MoveRight() bool {
	n := utf8.RuneCountInString(f.text)
	if f.caret >= n {
		f.caret = n
		return false
	}
	f.caret++
	return true
}

func (f *textField) Home() bool {
	if f.caret == 0 {
		return false
	}
	f.caret = 0
	return true
}

func (f *textField) End() bool {
	n := utf8.RuneCountInString(f.text)
	if f.caret == n {
		return false
	}
	f.caret = n
	return true
}

// DisplayWindow returns a horizontally scrolled slice of text+preedit around the
// caret that fits within maxRunes, along with the caret column and preedit span
// relative to the returned slice.
func (f *textField) DisplayWindow(maxRunes int) (visible string, caretCol, preStartCol, preEndCol int) {
	if maxRunes <= 0 {
		return "", 0, -1, -1
	}
	base := []rune(f.text)
	caret := clampCaret(f.caret, len(base))
	pre := []rune(f.preedit)

	combined := make([]rune, 0, len(base)+len(pre))
	combined = append(combined, base[:caret]...)
	combined = append(combined, pre...)
	combined = append(combined, base[caret:]...)

	cursor := caret + len(pre)
	start := 0
	if len(combined) > maxRunes {
		if cursor > maxRunes-1 {
			start = cursor - (maxRunes - 1)
		}
		if start+maxRunes > len(combined) {
			start = len(combined) - maxRunes
		}
		if start < 0 {
			start = 0
		}
	}
	end := start + maxRunes
	if end > len(combined) {
		end = len(combined)
	}

	visible = string(combined[start:end])
	caretCol = cursor - start
	if caretCol < 0 {
		caretCol = 0
	} else if caretCol > end-start {
		caretCol = end - start
	}

	if len(pre) == 0 {
		return visible, caretCol, -1, -1
	}
	pStart := caret - start
	pEnd := caret + len(pre) - start
	if pEnd <= 0 || pStart >= end-start {
		return visible, caretCol, -1, -1
	}
	if pStart < 0 {
		pStart = 0
	}
	if pEnd > end-start {
		pEnd = end - start
	}
	return visible, caretCol, pStart, pEnd
}

// UpdateIME pumps the Ebitengine IME Composer when focused. It returns whether
// the IME consumed keyboard input this tick, whether a commit occurred (so
// AppendInputChars should be skipped), whether text changed, and whether the
// platform ended text input.
func (f *textField) UpdateIME(focused bool, caretBounds image.Rectangle) (handled, committed, changed, ended bool) {
	f.caretBounds = caretBounds
	if !focused {
		if f.composer != nil && f.sessionActive {
			f.composer.Cancel()
			f.sessionActive = false
		}
		f.preedit = ""
		return false, false, false, false
	}
	f.ensureComposer()
	f.changed = false
	f.committedTick = false
	f.endedByUser = false

	h, err := f.composer.Update()
	if err != nil {
		return h, f.committedTick, f.changed, f.endedByUser
	}
	return h, f.committedTick, f.changed, f.endedByUser
}

func (f *textField) ConfirmIME() {
	if f.composer != nil && f.sessionActive {
		f.composer.Confirm()
		f.sessionActive = false
	}
}

func (f *textField) ensureComposer() {
	if f.composer != nil {
		return
	}
	f.composer = &textinput.Composer{
		OnNewSession: func() *textinput.SessionOptions {
			f.sessionActive = true
			runes := []rune(f.text)
			caret := clampCaret(f.caret, len(runes))
			return &textinput.SessionOptions{
				CaretBounds:     f.caretBounds,
				TextBeforeCaret: string(runes[:caret]),
				TextAfterCaret:  string(runes[caret:]),
			}
		},
		OnComposition: func(c *textinput.Composition) {
			if c == nil {
				f.preedit = ""
				f.preeditStart = 0
				f.preeditEnd = 0
				return
			}
			f.preedit = sanitizeSingleLine(c.Text())
			f.preeditStart, f.preeditEnd = c.SelectionRangeInBytes()
		},
		OnCommit: func(c *textinput.Commit) {
			f.preedit = ""
			f.preeditStart = 0
			f.preeditEnd = 0
			if c == nil {
				return
			}
			repBefore, repAfter := c.IsSurroundingTextReplaced()
			if repBefore || repAfter {
				before, after := c.SurroundingText()
				cleanBefore := sanitizeSingleLine(before)
				cleanText := sanitizeSingleLine(c.Text())
				cleanAfter := sanitizeSingleLine(after)
				f.text = cleanBefore + cleanText + cleanAfter
				f.caret = utf8.RuneCountInString(cleanBefore) + utf8.RuneCountInString(cleanText)
				f.changed = true
				f.committedTick = true
				return
			}
			if f.Insert(c.Text()) {
				f.changed = true
				f.committedTick = true
			}
		},
		OnEndByUser: func() {
			f.sessionActive = false
			f.preedit = ""
			f.endedByUser = true
		},
	}
}

func clampCaret(caret, n int) int {
	if caret < 0 {
		return 0
	}
	if caret > n {
		return n
	}
	return caret
}

func sanitizeSingleLine(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
	}
	return b.String()
}
