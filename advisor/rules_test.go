package advisor

import (
	"strings"
	"testing"
)

// TestNewAdvisorDoesNotPanic guards against any rule using a regex construct
// that Go's regexp (RE2) rejects at compile time (e.g. lookahead/lookbehind).
func TestNewAdvisorDoesNotPanic(t *testing.T) {
	a := NewAdvisor()
	if a == nil || len(a.rules) == 0 {
		t.Fatalf("NewAdvisor returned no rules")
	}
}

// TestDeleteAliasRule verifies Rule 26 matches aliased DELETE (with and without
// a trailing clause) and skips table-only DELETE statements.
func TestDeleteAliasRule(t *testing.T) {
	a := NewAdvisor()

	cases := []struct {
		sql  string
		want bool
	}{
		{"DELETE FROM orders o WHERE o.id = 1", true},   // aliased + WHERE
		{"DELETE FROM orders o ORDER BY o.id LIMIT 1", true}, // aliased + ORDER/LIMIT
		{"DELETE FROM orders o", true},                  // aliased, no clause
		{"DELETE FROM orders WHERE id = 1", false},      // no alias (WHERE is keyword)
		{"DELETE FROM t1 JOIN t2 ON t1.id = t2.id", false}, // multi-table delete
	}

	hits := func(sql string) bool {
		for _, r := range a.rules {
			if r.ID == 26 && r.Pattern.MatchString(sql) {
				return true
			}
		}
		return false
	}

	for _, c := range cases {
		if got := hits(c.sql); got != c.want {
			t.Fatalf("Rule 26 match(%q) = %v, want %v", c.sql, got, c.want)
		}
	}
}

// TestAllPatternsCompile is a belt-and-suspenders guard: ensure every rule's
// Pattern is already compiled (buildRules runs MustCompile on all of them).
func TestAllRulesHaveIDs(t *testing.T) {
	a := NewAdvisor()
	seen := make(map[int]bool)
	for _, r := range a.rules {
		if r.ID <= 0 {
			t.Fatalf("rule has non-positive ID: %+v", r)
		}
		if seen[r.ID] {
			t.Fatalf("duplicate rule ID %d", r.ID)
		}
		seen[r.ID] = true
		if r.Title == "" || strings.TrimSpace(r.Suggestion) == "" {
			t.Fatalf("rule %d missing title/suggestion", r.ID)
		}
	}
}
