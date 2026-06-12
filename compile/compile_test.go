package compile

import (
	"slices"
	"strings"
	"testing"
)

func TestRangeRunes(t *testing.T) {
	// Overlapping ranges deduplicate and sort.
	got := rangeRunes([]GlyphRange{{'a', 'c'}, {'b', 'd'}, {'0', '0'}})
	if want := []rune{'0', 'a', 'b', 'c', 'd'}; !slices.Equal(got, want) {
		t.Errorf("rangeRunes = %q, want %q", string(got), string(want))
	}
	if got := rangeRunes(nil); got != nil {
		t.Errorf("rangeRunes(nil) = %q, want nil", string(got))
	}
	// An inverted range (Lo > Hi) covers nothing.
	if got := rangeRunes([]GlyphRange{{'z', 'a'}}); got != nil {
		t.Errorf("inverted range = %q, want nil", string(got))
	}
}

func TestFormatRunes(t *testing.T) {
	if got := formatRunes([]rune{'A', 'b'}); got != "U+0041 U+0062" {
		t.Errorf("formatRunes = %q, want %q", got, "U+0041 U+0062")
	}
	many := make([]rune, 25)
	for i := range many {
		many[i] = rune('A' + i)
	}
	if got := formatRunes(many); !strings.Contains(got, "(+5 more)") {
		t.Errorf("formatRunes(25 runes) = %q, want it to contain %q", got, "(+5 more)")
	}
}
