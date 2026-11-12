package sql

import (
	"regexp"
	"strings"
)

// paramReplacer holds the ordered replacement rules for MyBatis parameters.
type paramReplacer struct {
	rules []replaceRule
}

type replaceRule struct {
	pattern     *regexp.Regexp
	replacement string
	description string
	replacer    func(match []string) string // if set, takes precedence over replacement
}

// newParamReplacer creates a replacer with the ordered rule substitution pipeline.
// Order is CRITICAL — earlier rules can preempt later ones.
func newParamReplacer() *paramReplacer {
	return &paramReplacer{
		rules: []replaceRule{
			// Rule 1: LIMIT #{offset}, #{count}  → LIMIT 10, 10  (MyBatis, longest first)
			{pattern: regexp.MustCompile(`(?i)limit\s+#\{[^}]*\}\s*,\s*#\{[^}]*\}`), replacement: "LIMIT 10, 10", description: "limit-offset-count"},
			// Rule 1b: LIMIT #s#, #c#  → LIMIT 10, 10  (iBATIS legacy comma form, must precede Rule 2)
			{pattern: regexp.MustCompile(`(?i)limit\s+#[^#\s]+\#\s*,\s*#[^#\s]+\#`), replacement: "LIMIT 10, 10", description: "legacy-limit-offset-count"},
			// Rule 2: LIMIT #{count}  → LIMIT 100
			{pattern: regexp.MustCompile(`(?i)limit\s+#\{?[^}\s]*\}?`), replacement: "LIMIT 100", description: "limit-count"},
			// Rule 2b: OFFSET <num>  → OFFSET 10  (iBATIS legacy #p# and MyBatis #{p}; must precede Rule 6/Rule 8)
			{pattern: regexp.MustCompile(`(?i)\boffset\s+(#\{[^}]*\}|#[^#\s]+\#)`), replacement: "OFFSET 10", description: "offset-number"},
			// Rule 2c: ROWNUM BETWEEN #a# AND #b# → ROWNUM BETWEEN 1 AND 20 (must precede Rule 6/Rule 8)
			{pattern: regexp.MustCompile(`(?i)\brownum\s+between\s+(#\{[^}]*\}|#[^#\s]+\#)\s+and\s+(#\{[^}]*\}|#[^#\s]+\#)`), replacement: "ROWNUM BETWEEN 1 AND 20", description: "rownum-between"},
			// Rule 2d: ROWNUM <op> #n# → ROWNUM <op> 10.  Preserves the comparison operator
			// (<=, <, >=, >, =) and replaces only the bound with an integer (Oracle-style
			// paging); must precede Rule 6/Rule 8.
			{pattern: regexp.MustCompile(`(?i)\brownum\s*(<=|>=|=|<>|<|>)?\s*(#\{[^}]*\}|#[^#\s]+\#)`), replacer: func(m []string) string {
				op := strings.TrimSpace(m[1])
				if op != "" {
					op = " " + op
				}
				return "ROWNUM" + op + " 10"
			}, description: "rownum-op-number"},
			// Rule 2e: FETCH FIRST #c# ROW(S) ONLY → FETCH FIRST 100 ROWS ONLY (DB2-style;
			// handles both singular "row only" and plural "rows only"; ${} rows-only is
			// already handled later by Rule 7, so this rule only covers #...# / #{...})
			{pattern: regexp.MustCompile(`(?i)fetch\s+first\s+(#\{[^}]*\}|#[^#\s]+\#)\s+rows?\s+only`), replacement: "FETCH FIRST 100 ROWS ONLY", description: "fetch-first-num"},
			// Rule 3: '#{dateParam}'  → '20230301'  (quoted dates matched before generic)
			{pattern: regexp.MustCompile(`'\s*#\{[^'}]*\}\s*'`), replacement: "'20230301'", description: "quoted-date"},
			// Rule 4: #{param,jdbcType=DATE/...} → '20230301'
			{pattern: regexp.MustCompile(`(?i)#\{[^}]*,\s*jdbcType\s*=\s*(DATE|TIME|TIMESTAMP)\s*\}`), replacement: "'20230301'", description: "jdbctype-date"},
			// Rule 5: '#legacyParam#'  → '20230302'  (legacy quoted before unquoted)
			{pattern: regexp.MustCompile(`'#[^']+#'`), replacement: "'20230302'", description: "legacy-quoted"},
			// Rule 6: #legacyParam#  → '20230302'  (legacy unquoted)
			{pattern: regexp.MustCompile(`#[^#\s]+#`), replacement: "'20230302'", description: "legacy-unquoted"},
			// Rule 7: ${param} ROWS ONLY  → 100 ROWS ONLY
			{pattern: regexp.MustCompile(`(?i)\$\{[^}]+\}\s+rows\s+only`), replacement: "100 ROWS ONLY", description: "fetch-rows"},
			// Rule 8: #{param}  → dummy value (generic — LAST)
			{pattern: regexp.MustCompile(`#\{[^}]*\}`), replacement: "'x'", description: "hash-placeholder"},
			// Rule 9: ${param}  → dummy string
			{pattern: regexp.MustCompile(`\$\{[^}]*\}`), replacement: "'t'", description: "dollar-placeholder"},
		},
	}
}

// ReplaceParams applies the ordered parameter replacement rules to a SQL string.
func ReplaceParams(sql string) string {
	r := newParamReplacer()
	result := sql
	for _, rule := range r.rules {
		if rule.replacer != nil {
			result = rule.pattern.ReplaceAllStringFunc(result, func(m string) string {
				return rule.replacer(rule.pattern.FindStringSubmatch(m))
			})
		} else {
			result = rule.pattern.ReplaceAllString(result, rule.replacement)
		}
	}

	// Post-processing: clean up whitespace
	result = strings.ReplaceAll(result, "\n where \n and ", "\n where ")
	result = strings.ReplaceAll(result, "\n where \n and", "\n where ")
	result = strings.ReplaceAll(result, "\n where  and ", "\n where ")

	// Collapse multiple spaces
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}

	return strings.TrimSpace(result)
}

// ExtractAndReplace extracts SQL from parsed statements and applies parameter substitution.
// Returns a copy of the statement with TestSQL populated.
func ExtractAndReplace(oldSQL string) string {
	return ReplaceParams(oldSQL)
}

// SQLTypeDummyVal returns a dummy value suitable for a given SQL type context.
func SQLTypeDummyVal(jdbcType string) string {
	switch strings.ToUpper(jdbcType) {
	case "VARCHAR", "CHAR", "CLOB", "TEXT", "MEDIUMTEXT", "LONGTEXT":
		return "'x'"
	case "INTEGER", "INT", "SMALLINT", "TINYINT", "MEDIUMINT":
		return "1"
	case "BIGINT":
		return "1"
	case "DECIMAL", "NUMERIC", "FLOAT", "DOUBLE", "REAL":
		return "1.0"
	case "DATE":
		return "'2023-01-01'"
	case "TIME":
		return "'12:00:00'"
	case "TIMESTAMP", "DATETIME":
		return "'2023-01-01 12:00:00'"
	case "BOOLEAN", "BIT":
		return "TRUE"
	case "BLOB", "BINARY", "VARBINARY":
		return "X'00'"
	default:
		return "'x'"
	}
}
