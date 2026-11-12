package advisor

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTruncateErrMultibyte guards the advisor copy of truncateErr: the message
// here ends up in SuggestionHint.Suggestion and then in the Excel report, so a
// 200-byte cut must not split a Chinese character.
func TestTruncateErrMultibyte(t *testing.T) {
	in := strings.Repeat("错误", 100) // 600 bytes
	got := truncateErr(in, 200)

	if !utf8.ValidString(got) {
		t.Fatalf("output is not valid UTF-8: %q", got)
	}
	// Contract is "maxLen bytes plus an ellipsis", not "at most maxLen".
	if len(got) > 200+len("...") {
		t.Fatalf("length %d exceeds maxLen+ellipsis (203)", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected an ellipsis suffix, got %q", got)
	}
	// 200 backs off to 198 (198 % 6 == 0), then "..." is appended.
	if len(got) != 198+3 {
		t.Fatalf("expected 201 bytes, got %d", len(got))
	}
}

func TestTruncateErrShortInputIsUnchanged(t *testing.T) {
	in := "ERROR 1064: syntax error"
	if got := truncateErr(in, 200); got != in {
		t.Fatalf("truncateErr(%q, 200) = %q, want input unchanged", in, got)
	}
	if strings.HasSuffix(truncateErr(in, 200), "...") {
		t.Fatalf("short input must not gain an ellipsis")
	}
}

func TestTruncateErrInvalidInput(t *testing.T) {
	got := truncateErr("bad \xff\xfe message", 200)
	if !utf8.ValidString(got) {
		t.Fatalf("output is not valid UTF-8: %q", got)
	}
}

func TestTruncateErrNonPositiveLimit(t *testing.T) {
	for _, maxLen := range []int{0, -1} {
		if got := truncateErr("something", maxLen); got != "" {
			t.Fatalf("maxLen=%d produced %q, want empty", maxLen, got)
		}
	}
}
