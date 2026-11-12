package advisor

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Rule represents a single rewrite suggestion rule.
type Rule struct {
	ID          int
	Pattern     *regexp.Regexp
	ErrorCode   string // TiDB error code to match (e.g., "1064", "1305")
	Category    string
	Title       string
	Suggestion  string
	Rewrite     string // optional: example rewrite
	Reference   string // optional: documentation link
}

// SuggestionResult holds the output of applying advisor rules.
type SuggestionResult struct {
	SQLID    string            `json:"sql_id"`
	Errors   []string          `json:"detected_issues"`
	Hints    []SuggestionHint  `json:"suggestions"`
}

// SuggestionHint is a single rewrite hint.
type SuggestionHint struct {
	Category   string `json:"category"`
	Title      string `json:"title"`
	Suggestion string `json:"suggestion"`
	Rewrite    string `json:"rewrite,omitempty"`
	Reference  string `json:"reference,omitempty"`
}

// Advisor applies compatibility rules to EXPLAIN failures.
type Advisor struct {
	rules []Rule
}

// NewAdvisor creates a new Advisor with the built-in rule set (≥16 rules).
func NewAdvisor() *Advisor {
	return &Advisor{rules: buildRules()}
}

// Analyze applies all matching rules to an EXPLAIN error and returns suggestions.
func (a *Advisor) Analyze(sqlID, sqlText, errMsg string) *SuggestionResult {
	result := &SuggestionResult{
		SQLID:  sqlID,
		Errors: []string{},
		Hints:  []SuggestionHint{},
	}

	if errMsg != "" {
		result.Errors = append(result.Errors, errMsg)
	}

	for _, rule := range a.rules {
		// Match by error code if specified
		if rule.ErrorCode != "" {
			codePattern := regexp.MustCompile(`(?i)ERROR\s*` + rule.ErrorCode)
			if !codePattern.MatchString(errMsg) {
				continue
			}
		}

		// Match by SQL pattern
		if rule.Pattern != nil && rule.Pattern.MatchString(sqlText) {
			result.Hints = append(result.Hints, SuggestionHint{
				Category:   rule.Category,
				Title:      rule.Title,
				Suggestion: rule.Suggestion,
				Rewrite:    rule.Rewrite,
				Reference:  rule.Reference,
			})
		}
	}

	// Fallback: generic analysis if no specific rules matched
	if len(result.Hints) == 0 && errMsg != "" {
		result.Hints = a.genericAnalyze(sqlText, errMsg)
	}

	return result
}

// ToJSON serializes suggestions to JSON for storage in the suggestion column.
func (sr *SuggestionResult) ToJSON() string {
	if sr == nil || len(sr.Hints) == 0 {
		return ""
	}
	data, _ := json.Marshal(sr.Hints)
	return string(data)
}

// buildRules constructs the built-in rule set.
func buildRules() []Rule {
	return []Rule{
		// Rule 1: SYSDATE() → NOW()
		{
			ID: 1, ErrorCode: "1305",
			Pattern:    regexp.MustCompile(`(?i)SYSDATE\s*\(`),
			Category:   "function-replacement",
			Title:      "SYSDATE() 不支持",
			Suggestion: "SYSDATE() 在 TiDB 中不存在，改用 NOW()（语句开始时间）",
			Rewrite:    "SYSDATE() → NOW()",
			Reference:  "https://docs.pingcap.com/tidb/stable/date-and-time-functions",
		},
		// Rule 2: SQL_CALC_FOUND_ROWS
		{
			ID: 2, ErrorCode: "1064",
			Pattern:    regexp.MustCompile(`(?i)SQL_CALC_FOUND_ROWS`),
			Category:   "syntax-removal",
			Title:      "SQL_CALC_FOUND_ROWS 不支持",
			Suggestion: "去掉 SQL_CALC_FOUND_ROWS；改用 COUNT(*) 单独查询总数",
			Rewrite:    "移除 SQL_CALC_FOUND_ROWS",
		},
		// Rule 3: INSERT DELAYED
		{
			ID: 3,
			Pattern:    regexp.MustCompile(`(?i)INSERT\s+DELAYED`),
			Category:   "syntax-removal",
			Title:      "INSERT DELAYED 不支持",
			Suggestion: "TiDB 不支持 INSERT DELAYED，去掉 DELAYED 关键字",
			Rewrite:    "INSERT DELAYED → INSERT",
		},
		// Rule 4: FULLTEXT index
		{
			ID: 4,
			Pattern:    regexp.MustCompile(`(?i)FULLTEXT\s+(INDEX|KEY)`),
			Category:   "unsupported-feature",
			Title:      "全文索引不支持",
			Suggestion: "TiDB 不支持 FULLTEXT 索引，需要评估替代方案（如应用层全文搜索）",
			Reference:  "https://docs.pingcap.com/tidb/stable/mysql-compatibility",
		},
		// Rule 5: SPATIAL index
		{
			ID: 5,
			Pattern:    regexp.MustCompile(`(?i)SPATIAL\s+(INDEX|KEY)`),
			Category:   "unsupported-feature",
			Title:      "空间索引不支持",
			Suggestion: "TiDB 不支持 SPATIAL 索引",
		},
		// Rule 6: CREATE PROCEDURE / FUNCTION / TRIGGER
		{
			ID: 6,
			Pattern:    regexp.MustCompile(`(?i)CREATE\s+(PROCEDURE|FUNCTION|TRIGGER|EVENT)`),
			Category:   "unsupported-feature",
			Title:      "存储过程/函数/触发器不支持",
			Suggestion: "TiDB 暂不支持存储过程、函数、触发器和事件，需迁移到应用层逻辑",
			Reference:  "https://docs.pingcap.com/tidb/stable/stored-procedures",
		},
		// Rule 7: FOR UPDATE NOWAIT
		{
			ID: 7,
			Pattern:    regexp.MustCompile(`(?i)FOR\s+UPDATE\s+NOWAIT`),
			Category:   "version-requirement",
			Title:      "FOR UPDATE NOWAIT 版本要求",
			Suggestion: "TiDB 6.0+ 支持 NOWAIT，低版本需去掉 NOWAIT 或升级",
			Rewrite:    "FOR UPDATE NOWAIT → FOR UPDATE (需 TiDB ≥ 6.0)",
		},
		// Rule 8: LOCK IN SHARE MODE
		{
			ID: 8,
			Pattern:    regexp.MustCompile(`(?i)LOCK\s+IN\s+SHARE\s+MODE`),
			Category:   "syntax-update",
			Title:      "LOCK IN SHARE MODE 建议改用 FOR SHARE",
			Suggestion: "MySQL 8.0+ 已弃用 LOCK IN SHARE MODE，建议改用 FOR SHARE",
			Rewrite:    "LOCK IN SHARE MODE → FOR SHARE",
		},
		// Rule 9: GROUP BY WITH ROLLUP
		{
			ID: 9,
			Pattern:    regexp.MustCompile(`(?i)GROUP\s+BY\s+.+\s+WITH\s+ROLLUP`),
			Category:   "behavior-difference",
			Title:      "WITH ROLLUP 支持有限",
			Suggestion: "TiDB 对 WITH ROLLUP 的支持有限，建议在应用层做汇总计算",
		},
		// Rule 10: PARTITION BY
		{
			ID: 10,
			Pattern:    regexp.MustCompile(`(?i)PARTITION\s+BY`),
			Category:   "behavior-difference",
			Title:      "分区语法差异",
			Suggestion: "TiDB 分区不支持 HASH 子分区、KEY 分区等；请使用 RANGE/LIST/HASH 分区",
			Reference:  "https://docs.pingcap.com/tidb/stable/partitioned-table",
		},
		// Rule 11: REPLACE INTO
		{
			ID: 11,
			Pattern:    regexp.MustCompile(`(?i)REPLACE\s+INTO`),
			Category:   "behavior-difference",
			Title:      "REPLACE INTO 行为差异",
			Suggestion: "REPLACE INTO 在 TiDB 中的行为可能与 MySQL 有差异（如自增 ID 分配），建议评估",
		},
		// Rule 12: Window functions
		{
			ID: 12,
			Pattern:    regexp.MustCompile(`(?i)OVER\s*\(`),
			Category:   "behavior-difference",
			Title:      "窗口函数兼容性",
			Suggestion: "部分窗口函数（如 RANK/ROW_NUMBER）TiDB 支持，但 CUME_DIST/PERCENT_RANK 等可能不支持",
			Reference:  "https://docs.pingcap.com/tidb/stable/window-functions",
		},
		// Rule 13: CTE WITH ... AS
		{
			ID: 13,
			Pattern:    regexp.MustCompile(`(?i)WITH\s+\w+\s+AS\s*\(`),
			Category:   "version-requirement",
			Title:      "CTE 版本要求",
			Suggestion: "CTE (Common Table Expression) 需 TiDB 5.x+，低版本不支持",
			Reference:  "https://docs.pingcap.com/tidb/stable/dev-guide-use-common-table-expression",
		},
		// Rule 14: MySQL version-conditional comments
		{
			ID: 14,
			Pattern:    regexp.MustCompile(`/\*!\d{5}`),
			Category:   "syntax-removal",
			Title:      "MySQL 版本条件注释",
			Suggestion: "检测到 /*!XXXXX ... */ 语法，内部语句在 TiDB 中会执行但可能不兼容，建议评估",
		},
		// Rule 15: ON DUPLICATE KEY UPDATE
		{
			ID: 15,
			Pattern:    regexp.MustCompile(`(?i)ON\s+DUPLICATE\s+KEY\s+UPDATE`),
			Category:   "behavior-difference",
			Title:      "ON DUPLICATE KEY UPDATE 行为差异",
			Suggestion: "ON DUPLICATE KEY UPDATE 在 TiDB 中可能与 MySQL 有行为差异（自增 ID 消耗不同）",
		},
		// Rule 16: User-defined variables (@var)
		{
			ID: 16,
			Pattern:    regexp.MustCompile(`(?i)SET\s+@\w+\s*[:=]|SELECT\s+@\w+`),
			Category:   "unsupported-feature",
			Title:      "用户自定义变量 (UDV) 不支持",
			Suggestion: "TiDB 不支持用户自定义变量 (@var)，建议使用 CTE 或临时表替代",
			Reference:  "https://docs.pingcap.com/tidb/stable/user-defined-variables",
		},

		// ===== Oracle → TiDB 专属规则 =====

		// Rule 17: TO_CHAR / TO_NUMBER → CONVERT
		{
			ID: 17,
			Pattern:    regexp.MustCompile(`(?i)\bTO_CHAR\s*\(|\bTO_NUMBER\s*\(`),
			Category:   "oracle-function-replacement",
			Title:      "Oracle TO_CHAR / TO_NUMBER → CONVERT",
			Suggestion: "Oracle TO_CHAR/TO_NUMBER 需改为 TiDB CONVERT(key, dataType)。支持类型: BINARY, CHAR, DATE, DATETIME, TIME, SIGNED INTEGER, UNSIGNED INTEGER, DECIMAL",
			Rewrite:    "TO_CHAR(col) → CONVERT(col, CHAR)\nTO_NUMBER(col) → CONVERT(col, DECIMAL)",
		},
		// Rule 18: TO_DATE → STR_TO_DATE (format string case-sensitive)
		{
			ID: 18,
			Pattern:    regexp.MustCompile(`(?i)\bTO_DATE\s*\(`),
			Category:   "oracle-function-replacement",
			Title:      "Oracle TO_DATE → STR_TO_DATE（格式大小写敏感）",
			Suggestion: "Oracle TO_DATE(str, 'yyyy-MM-dd') 改为 TiDB STR_TO_DATE(str, '%Y-%m-%d')。注意: Oracle 格式不区分大小写，TiDB %Y/%m/%d 必须大写，%i/%s 必须小写",
			Rewrite:    "TO_DATE('2021-05-28', 'yyyy-MM-dd hh24:mi:ss') → STR_TO_DATE('2021-05-28', '%Y-%m-%d %H:%i:%s')",
		},
		// Rule 19: NVL → IFNULL / NVL2 → IF
		{
			ID: 19,
			Pattern:    regexp.MustCompile(`(?i)\bNVL2?\s*\(`),
			Category:   "oracle-function-replacement",
			Title:      "Oracle NVL / NVL2 → IFNULL / IF",
			Suggestion: "NVL(expr, val) → IFNULL(expr, val)；NVL2(expr, v1, v2) → IF(expr IS NULL, v1, v2)。注意 TiDB 中 NULL 和空串''是不同的",
			Rewrite:    "NVL(col, 0) → IFNULL(col, 0)\nNVL2(col, 'A', 'B') → IF(col IS NULL, 'A', 'B')",
		},
		// Rule 20: DECODE → CASE WHEN / IF
		{
			ID: 20,
			Pattern:    regexp.MustCompile(`(?i)\bDECODE\s*\(`),
			Category:   "oracle-function-replacement",
			Title:      "Oracle DECODE → CASE WHEN 或 IF",
			Suggestion: "DECODE(value, if1, val1, if2, val2, ..., default) → CASE WHEN value=if1 THEN val1 WHEN value=if2 THEN val2 ... ELSE default END。简单两值场景可用 IF(key=val1, val2, val3)",
			Rewrite:    "DECODE(status, 'A', 'Active', 'B', 'Blocked', 'Unknown') → CASE WHEN status='A' THEN 'Active' WHEN status='B' THEN 'Blocked' ELSE 'Unknown' END",
		},
		// Rule 21: Oracle (+) outer join operator
		{
			ID: 21,
			Pattern:    regexp.MustCompile(`((?i)\s*\(\s*\+\s*\)\s*)`),
			Category:   "oracle-syntax-replacement",
			Title:      "Oracle (+) 外连接 → LEFT/RIGHT JOIN",
			Suggestion: "Oracle 使用 t1.id(+) = t2.id 实现右外连接，TiDB 不支持，改为 LEFT JOIN / RIGHT JOIN 语法",
			Rewrite:    "FROM a, b WHERE a.id = b.id(+) → FROM a LEFT JOIN b ON a.id = b.id",
		},
		// Rule 22: ROWNUM → LIMIT / ROW_NUMBER
		{
			ID: 22,
			Pattern:    regexp.MustCompile(`(?i)\bROWNUM\b`),
			Category:   "oracle-syntax-replacement",
			Title:      "Oracle ROWNUM → LIMIT 或 ROW_NUMBER()",
			Suggestion: "ROWNUM 不支持，简单分页场景用 LIMIT 替换，复杂排序场景用 ROW_NUMBER() OVER(ORDER BY ...) 窗口函数。注意 Oracle ROWNUM 在 ORDER BY 之前计算，行为有差异",
			Rewrite:    "WHERE ROWNUM <= 10 → LIMIT 10\nSELECT ROWNUM, ... → SELECT ROW_NUMBER() OVER(ORDER BY col) rownum, ...",
		},
		// Rule 23: CONNECT BY → WITH RECURSIVE
		{
			ID: 23,
			Pattern:    regexp.MustCompile(`(?i)\bCONNECT\s+BY\b`),
			Category:   "oracle-syntax-replacement",
			Title:      "Oracle CONNECT BY → WITH RECURSIVE CTE",
			Suggestion: "Oracle 层次查询 CONNECT BY PRIOR 需改为 WITH RECURSIVE 公共表达式实现。注意处理 LEVEL 伪列和 NOCYCLE 选项",
			Rewrite:    "SELECT ... CONNECT BY PRIOR id=pid → WITH RECURSIVE cte AS (SELECT 1 level, t.* FROM t UNION ALL SELECT level+1, t.* FROM cte r JOIN t ON r.id=t.pid) SELECT * FROM cte",
			Reference:  "https://docs.pingcap.com/tidb/stable/dev-guide-use-common-table-expression",
		},
		// Rule 24: MINUS → EXCEPT
		{
			ID: 24,
			Pattern:    regexp.MustCompile(`(?i)\bMINUS\b`),
			Category:   "oracle-syntax-replacement",
			Title:      "Oracle MINUS → EXCEPT",
			Suggestion: "TiDB 不支持 MINUS 差集运算，需改为 EXCEPT",
			Rewrite:    "SELECT ... MINUS SELECT ... → SELECT ... EXCEPT SELECT ...",
		},
		// Rule 25: String concatenation || → CONCAT
		{
			ID: 25,
			Pattern:    regexp.MustCompile(`\|\|`),
			Category:   "oracle-function-replacement",
			Title:      "Oracle || 字符串拼接 → CONCAT",
			Suggestion: "Oracle 使用 || 拼接字符串，TiDB 使用 CONCAT(a, b, ...)。注意 TiDB 中 CONCAT 任一参数为 NULL 则返回 NULL，建议结合 IFNULL 使用",
			Rewrite:    "col1 || ' - ' || col2 → CONCAT(col1, ' - ', col2)\n如果可能有 NULL: CONCAT(IFNULL(col1, ''), ' - ', IFNULL(col2, ''))",
		},
		// Rule 26: DELETE with table alias
		{
			ID: 26,
			// Go's regexp (RE2) has no lookahead, so match the aliased-delete shape
			// positively: "DELETE FROM <table> <alias>" where the alias is followed
			// by a WHERE/ORDER/LIMIT clause or the end of the statement. This avoids
			// false positives on "DELETE FROM t WHERE ..." (WHERE is a keyword, not an
			// alias) and on "DELETE FROM t1 JOIN t2 ..." multi-table syntax.
			Pattern:    regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+[\w.]+\s+[\w.]+\s*(?:WHERE|ORDER|LIMIT|\z)`),
			Category:   "oracle-syntax-removal",
			Title:      "DELETE 不支持表别名",
			Suggestion: "TiDB DELETE 语句不支持给表起别名，需去掉别名。多表删除用 DELETE t1 FROM t1 JOIN t2 ON ... 语法",
			Rewrite:    "DELETE FROM test t WHERE t.id=1 → DELETE FROM test WHERE id=1",
		},
		// Rule 27: MERGE INTO → INSERT ON DUPLICATE KEY UPDATE
		{
			ID: 27,
			Pattern:    regexp.MustCompile(`(?i)\bMERGE\s+INTO\b`),
			Category:   "oracle-syntax-replacement",
			Title:      "Oracle MERGE INTO → INSERT ON DUPLICATE KEY UPDATE",
			Suggestion: "TiDB 不支持 MERGE INTO，改为 INSERT ... ON DUPLICATE KEY UPDATE 或 REPLACE INTO。注意二者行为差异：ON DUPLICATE KEY UPDATE 保留原行再更新，REPLACE INTO 先删后插",
			Rewrite:    "MERGE INTO t USING src ON (t.id=src.id) WHEN MATCHED THEN UPDATE SET ... WHEN NOT MATCHED THEN INSERT ... → INSERT INTO t VALUES(...) ON DUPLICATE KEY UPDATE ...",
		},
		// Rule 28: FULL OUTER JOIN → UNION of LEFT+RIGHT
		{
			ID: 28,
			Pattern:    regexp.MustCompile(`(?i)\bFULL\s+(OUTER\s+)?JOIN\b`),
			Category:   "oracle-syntax-replacement",
			Title:      "FULL OUTER JOIN → LEFT JOIN UNION RIGHT JOIN",
			Suggestion: "TiDB 不支持 FULL OUTER JOIN，改为 LEFT JOIN UNION ALL RIGHT JOIN (NOT EXISTS 去重)。也可直接用 UNION 保持去重",
			Rewrite:    "FULL OUTER JOIN → (SELECT ... LEFT JOIN ... UNION ALL SELECT ... RIGHT JOIN ... WHERE NOT EXISTS ...)",
		},
		// Rule 29: LENGTH → CHAR_LENGTH
		{
			ID: 29,
			Pattern:    regexp.MustCompile(`(?i)\bLENGTH\s*\(`),
			Category:   "oracle-function-replacement",
			Title:      "LENGTH() 返回字节数，应用 CHAR_LENGTH()",
			Suggestion: "Oracle LENGTH 返回字符数，TiDB LENGTH 返回字节数（中文/UTF-8 一个字符 3 字节）。如需字符数请用 CHAR_LENGTH()",
			Rewrite:    "LENGTH(col) → CHAR_LENGTH(col)（如需字符数）",
		},
		// Rule 30: Oracle 空串即 NULL 差异
		{
			ID: 30,
			Pattern:    regexp.MustCompile(`(?i)=\s*''|''\s*=`),
			Category:   "oracle-null-difference",
			Title:      "Oracle '''' 等于 NULL，TiDB 空串和 NULL 不同",
			Suggestion: "Oracle 中将空串''视为 NULL，但 TiDB 中''是空字符串，NULL 是 NULL。导出 Oracle 数据时 NULL 可能转换为''，需检查并统一处理",
			Rewrite:    "WHERE col = '' → WHERE col IS NULL（如果业务上空串等价 NULL）",
		},
		// Rule 31: SUBSTR index starts at 0 in Oracle
		{
			ID: 31,
			Pattern:    regexp.MustCompile(`(?i)\bSUBSTR\b\s*\(`),
			Category:   "oracle-function-replacement",
			Title:      "SUBSTR 下标差异：Oracle 从 0 开始，TiDB 从 1 开始",
			Suggestion: "Oracle SUBSTR(str, 0, 2) 等价于 SUBSTR(str, 1, 2)，但 TiDB 中 SUBSTR(str, 0, 2) 返回空串。必须确保起始位置 ≥ 1",
			Rewrite:    "SUBSTR(col, 0, 2) → SUBSTRING(col, 1, 2)（TiDB 从 1 开始）",
		},
		// Rule 32: LISTAGG → GROUP_CONCAT
		{
			ID: 32,
			Pattern:    regexp.MustCompile(`(?i)\bLISTAGG\s*\(`),
			Category:   "oracle-function-replacement",
			Title:      "Oracle LISTAGG → GROUP_CONCAT",
			Suggestion: "Oracle LISTAGG(col, sep) WITHIN GROUP(ORDER BY ...) 改为 TiDB GROUP_CONCAT(col ORDER BY ... SEPARATOR sep)",
			Rewrite:    "LISTAGG(name, ',') WITHIN GROUP(ORDER BY id) → GROUP_CONCAT(name ORDER BY id SEPARATOR ',')",
		},
		// Rule 33: TRUNC → TRUNCATE / CAST
		{
			ID: 33,
			Pattern:    regexp.MustCompile(`(?i)\bTRUNC\b\s*\(`),
			Category:   "oracle-function-replacement",
			Title:      "Oracle TRUNC → TRUNCATE(数字) / CAST(日期)",
			Suggestion: "数字截断: TRUNC(n, m) → TRUNCATE(n, m)；日期截断: TRUNC(sysdate) → CAST(NOW() AS DATE)；月初: TRUNC(date, 'MM') → DATE_ADD(CURDATE(), INTERVAL -DAY(CURDATE())+1 DAY)",
			Rewrite:    "TRUNC(2.136, 2) → TRUNCATE(2.136, 2)\nTRUNC(sysdate) → CAST(NOW() AS DATE)\nTRUNC(sysdate, 'MM') → DATE_ADD(CURDATE(), INTERVAL -DAY(CURDATE())+1 DAY)",
		},
		// Rule 34: ADD_MONTHS → DATE_ADD
		{
			ID: 34,
			Pattern:    regexp.MustCompile(`(?i)\bADD_MONTHS\s*\(`),
			Category:   "oracle-function-replacement",
			Title:      "Oracle ADD_MONTHS → DATE_ADD",
			Suggestion: "ADD_MONTHS(date, n) → DATE_ADD(date, INTERVAL n MONTH)。注意月末行为差异: Oracle ADD_MONTHS 有月末自适应，TiDB DATE_ADD 无此行为",
			Rewrite:    "ADD_MONTHS(date, 3) → DATE_ADD(date, INTERVAL 3 MONTH)",
		},
		// Rule 35: SYSTIMESTAMP → CURRENT_TIMESTAMP(6)
		{
			ID: 35,
			Pattern:    regexp.MustCompile(`(?i)\bSYSTIMESTAMP\b`),
			Category:   "oracle-function-replacement",
			Title:      "Oracle SYSTIMESTAMP → CURRENT_TIMESTAMP(6)",
			Suggestion: "Oracle SYSTIMESTAMP 返回带微秒的时间戳，TiDB 使用 CURRENT_TIMESTAMP(6) 或 NOW(6) 替代",
			Rewrite:    "SYSTIMESTAMP → CURRENT_TIMESTAMP(6)",
		},
		// Rule 36: SYS_GUID → UUID
		{
			ID: 36,
			Pattern:    regexp.MustCompile(`(?i)\bSYS_GUID\s*\(\)`),
			Category:   "oracle-function-replacement",
			Title:      "Oracle SYS_GUID → UUID",
			Suggestion: "SYS_GUID() → UUID()，返回通用唯一识别码",
			Rewrite:    "SYS_GUID() → UUID()",
		},
		// Rule 37: ORDER BY NULLS FIRST/LAST
		{
			ID: 37,
			Pattern:    regexp.MustCompile(`(?i)\bNULLS\s+(FIRST|LAST)\b`),
			Category:   "oracle-null-difference",
			Title:      "NULLS FIRST / LAST 排序差异",
			Suggestion: "Oracle 和 TiDB NULL 排序默认行为相反: Oracle ASC 时 NULL 在后，TiDB ASC 时 NULL 在前。NULLS FIRST: TiDB 默认行为，无需改写。NULLS LAST: ISNULL(col) 排序技巧",
			Rewrite:    "ORDER BY col ASC NULLS LAST → ORDER BY ISNULL(col), col\nORDER BY col DESC NULLS FIRST → ORDER BY ISNULL(col) DESC, col DESC",
		},
		// Rule 38: Derived table must have alias (detected via syntax error pattern in genericAnalyze)
		{
			ID: 38,
			Pattern:    nil, // detected in genericAnalyze via error 1064 + nested SELECT pattern
			Category:   "oracle-syntax-requirement",
			Title:      "派生表/子查询必须加别名",
			Suggestion: "TiDB 中每个派生表（子查询）都必须有自己的别名，Oracle 允许省略别名。写法: SELECT * FROM (SELECT * FROM t) alias",
			Rewrite:    "SELECT * FROM (SELECT * FROM t) → SELECT * FROM (SELECT * FROM t) alias_name",
		},
		// Rule 39: INSERT from same table needs alias
		{
			ID: 39,
			Pattern:    regexp.MustCompile(`(?i)INSERT\s+INTO\s+\w+\s+.*SELECT\s+.*FROM\s+\w+\s*WHERE`),
			Category:   "oracle-syntax-requirement",
			Title:      "同表 INSERT...SELECT 需别名",
			Suggestion: "TiDB 不支持在 INSERT INTO t VALUES(...(SELECT ... FROM t)) 中同一语句查改同表，子查询表需加别名",
			Rewrite:    "INSERT INTO t VALUES(col1, (SELECT col2 FROM t WHERE ...)) → INSERT INTO t VALUES(col1, (SELECT alias.col2 FROM t alias WHERE ...))",
		},
	}
}

// genericAnalyze provides fallback suggestions when no specific rule matches.
func (a *Advisor) genericAnalyze(sqlText, errMsg string) []SuggestionHint {
	var hints []SuggestionHint

	// Check for common patterns
	lowerSQL := strings.ToLower(sqlText)
	lowerErr := strings.ToLower(errMsg)

	if strings.Contains(lowerErr, "syntax error") || strings.Contains(lowerErr, "1064") {
		// Check for MySQL-specific features
		if strings.Contains(lowerSQL, "use index") || strings.Contains(lowerSQL, "force index") {
			hints = append(hints, SuggestionHint{
				Category:   "syntax-removal",
				Title:      "索引提示",
				Suggestion: "USE INDEX/FORCE INDEX/IGNORE INDEX 在 TiDB 中不被推荐，建议由优化器自动选择",
			})
		}
		if strings.Contains(lowerSQL, "straight_join") {
			hints = append(hints, SuggestionHint{
				Category:   "syntax-removal",
				Title:      "STRAIGHT_JOIN",
				Suggestion: "STRAIGHT_JOIN 在 TiDB 中可能不被支持，建议移除",
			})
		}
			// Detect derived table missing alias: "FROM (SELECT ... )" without alias after )
			if strings.Contains(lowerSQL, ")") && strings.Contains(lowerSQL, "select") {
				hints = append(hints, SuggestionHint{
					Category:   "oracle-syntax-requirement",
					Title:      "派生表/子查询必须加别名",
					Suggestion: "TiDB 中每个派生表（子查询）都必须有自己的别名。如果错误涉及嵌套查询，检查 FROM (...) 之后是否缺少别名",
					Rewrite:    "SELECT * FROM (SELECT * FROM t) → SELECT * FROM (SELECT * FROM t) alias_name",
				})
			}
	}

	if len(hints) == 0 {
		hints = append(hints, SuggestionHint{
			Category:   "unknown",
			Title:      "未识别的错误",
			Suggestion: fmt.Sprintf("请人工检查此 SQL 的兼容性。错误信息：%s", truncateErr(errMsg, 200)),
		})
	}

	return hints
}

func truncateErr(err string, maxLen int) string {
	if len(err) > maxLen {
		return err[:maxLen] + "..."
	}
	return err
}
