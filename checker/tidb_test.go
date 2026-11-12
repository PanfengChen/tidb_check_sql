package checker

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateErr(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{
			name:   "empty",
			in:     "",
			maxLen: 512,
			want:   "",
		},
		{
			name:   "shorter than limit",
			in:     "Table 'foo' doesn't exist",
			maxLen: 512,
			want:   "Table 'foo' doesn't exist",
		},
		{
			name:   "exactly at limit",
			in:     "abc",
			maxLen: 3,
			want:   "abc",
		},
		{
			name:   "quotes are passed through, not escaped",
			in:     "it's a 'quoted' error",
			maxLen: 512,
			want:   "it's a 'quoted' error",
		},
		{
			name:   "truncated at ASCII boundary",
			in:     strings.Repeat("a", 600),
			maxLen: 512,
			want:   strings.Repeat("a", 512),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateErr(tt.in, tt.maxLen)
			if got != tt.want {
				t.Fatalf("truncateErr(%q, %d) = %q, want %q", tt.in, tt.maxLen, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("truncateErr(%q, %d) produced invalid UTF-8: %q", tt.in, tt.maxLen, got)
			}
			if len(got) > tt.maxLen {
				t.Fatalf("truncateErr(%q, %d) returned %d bytes, exceeds limit", tt.in, tt.maxLen, len(got))
			}
		})
	}
}

// TestTruncateErrMultibyteBoundary guards the regression where a 512-byte
// cut landed mid-rune and emitted invalid UTF-8 for Chinese error messages.
func TestTruncateErrMultibyteBoundary(t *testing.T) {
	// Each "错误" is 6 bytes; 200 repeats = 1200 bytes, so any even 512-byte
	// cut falls inside a rune.
	in := strings.Repeat("错误", 200)
	got := truncateErr(in, 512)

	if !utf8.ValidString(got) {
		t.Fatalf("truncated message is not valid UTF-8: %q", got)
	}
	if len(got) > 512 {
		t.Fatalf("length %d exceeds limit 512", len(got))
	}
	if !strings.HasPrefix(in, got) {
		t.Fatalf("truncated message is not a prefix of the input")
	}
	// Must back off to a rune boundary: 512 -> 510 (510 % 6 == 0).
	if len(got) != 510 {
		t.Fatalf("expected 510 bytes after backing off to rune boundary, got %d", len(got))
	}
}

// TestTruncateErrQuotesAreNotEscaped pins the removal of the old
// ReplaceAll("'", "''"): the message is stored through a bound parameter, so
// doubling quotes only corrupted what the operator reads back.
func TestTruncateErrQuotesAreNotEscaped(t *testing.T) {
	in := strings.Repeat("'", 600)
	got := truncateErr(in, 512)

	if len(got) > 512 {
		t.Fatalf("length %d exceeds limit 512", len(got))
	}
	if got != strings.Repeat("'", 512) {
		t.Fatalf("quotes were altered, got %d bytes", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("output is not valid UTF-8")
	}
}

// TestTruncateErrInvalidInput covers messages that arrive already corrupted,
// e.g. a TiDB connection using a non-utf8mb4 charset. The contract is "output
// is always valid UTF-8 and never exceeds the limit", not "output is a byte
// prefix of the input".
func TestTruncateErrInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		maxLen int
	}{
		{
			name:   "invalid byte under the limit",
			in:     "bad \xff\xfe message",
			maxLen: 512,
		},
		{
			name:   "invalid byte inside the cut window",
			in:     strings.Repeat("x", 500) + "\xff\xfe" + strings.Repeat("y", 100),
			maxLen: 512,
		},
		{
			name:   "all continuation bytes",
			in:     strings.Repeat("\x80", 600),
			maxLen: 512,
		},
		{
			name:   "truncated multi-byte sequence at the cut",
			in:     strings.Repeat("a", 510) + "错",
			maxLen: 511,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateErr(tt.in, tt.maxLen)
			if !utf8.ValidString(got) {
				t.Fatalf("output is not valid UTF-8: %q", got)
			}
			if len(got) > tt.maxLen {
				t.Fatalf("length %d exceeds limit %d", len(got), tt.maxLen)
			}
		})
	}
}

// TestTruncateErrAllContinuationBytes pins the degenerate path: when every
// candidate cut lands on a continuation byte, the loop walks to 0 rather than
// panicking or slicing out of range.
func TestTruncateErrAllContinuationBytes(t *testing.T) {
	got := truncateErr(strings.Repeat("\x80", 600), 512)
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
	if len(got) > 512 {
		t.Fatalf("length %d exceeds limit", len(got))
	}
}

func TestTruncateErrZeroAndNegativeLimit(t *testing.T) {
	for _, maxLen := range []int{0, -1} {
		got := truncateErr("something", maxLen)
		if got != "" {
			t.Fatalf("maxLen=%d produced %q, want empty", maxLen, got)
		}
	}
}
