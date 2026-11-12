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
			want:   "Table ''foo'' doesn''t exist",
		},
		{
			name:   "exactly at limit",
			in:     "abc",
			maxLen: 3,
			want:   "abc",
		},
		{
			name:   "quotes doubled for SQL literal",
			in:     "it's a 'quoted' error",
			maxLen: 512,
			want:   "it''s a ''quoted'' error",
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

func TestTruncateErrKeepsLimitAfterEscaping(t *testing.T) {
	// Escaping doubles every quote, so the limit is applied to the escaped
	// string. A long message full of quotes must still fit the budget.
	in := strings.Repeat("'", 400) // 400 bytes -> 800 bytes escaped
	got := truncateErr(in, 512)

	if len(got) > 512 {
		t.Fatalf("length %d exceeds limit 512", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("output is not valid UTF-8")
	}
}
