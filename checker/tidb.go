package checker

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"

	"check_sql_go/parser"
)

// DBCheckResult holds the result of an EXPLAIN check on a single SQL statement.
type DBCheckResult struct {
	*parser.SQLStatement
	Matched    string
	ErrMsg     string
	Suggestion string
}

// DBConfig holds connection parameters.
type DBConfig struct {
	Host   string
	Port   int
	User   string
	Passwd string
	Schema string
	Table  string
}

// Checker executes EXPLAIN statements against a TiDB/MySQL database.
type Checker struct {
	db     *sql.DB
	config DBConfig
}

// NewChecker creates a new Checker and connects to the database.
func NewChecker(cfg DBConfig) (*Checker, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/", cfg.User, cfg.Passwd, cfg.Host, cfg.Port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("cannot open database connection: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("cannot connect to database: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	return &Checker{db: db, config: cfg}, nil
}

// Close closes the database connection.
func (c *Checker) Close() {
	if c.db != nil {
		c.db.Close()
	}
}

// InitTable creates the result table if it does not exist and upgrades tables
// created by older versions.
//
// CREATE TABLE IF NOT EXISTS is a no-op on an existing table, so a table built
// before the suggestion column existed would silently keep the old shape and
// every UpdateResult would fail with "Unknown column" — which main only logs.
// The ALTER below is therefore part of the schema contract, not an optional
// nicety; duplicate-column errors are ignored so it stays idempotent.
func (c *Checker) InitTable() error {
	createSQL := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (
			sys VARCHAR(10) NOT NULL,
			modname VARCHAR(100) NOT NULL,
			sqlid VARCHAR(100) NOT NULL,
			oldsql MEDIUMTEXT,
			testsql MEDIUMTEXT,
			errmsg VARCHAR(512),
			matched CHAR(2),
			remark VARCHAR(512),
			suggestion TEXT,
			PRIMARY KEY (sys, modname, sqlid)
		)`, c.config.Table)
	_, err := c.db.Exec(createSQL)
	if err != nil {
		return fmt.Errorf("cannot create table %s: %w", c.config.Table, err)
	}

	alterSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN suggestion TEXT`, c.config.Table)
	if _, err := c.db.Exec(alterSQL); err != nil && !isDuplicateColumnErr(err) {
		return fmt.Errorf("cannot add suggestion column to %s: %w", c.config.Table, err)
	}
	return nil
}

// isDuplicateColumnErr reports whether err is MySQL/TiDB error 1060 (duplicate
// column), which is the expected outcome when the column already exists. The
// driver's typed error is checked first; the string match is a fallback for
// wrapped or non-standard error paths.
func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1060 {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}

// InsertOrUpdateSQL stores extracted SQL into the result table (phase 1: initSqls).
func (c *Checker) InsertOrUpdateSQL(sys, modname, sqlid, oldSQL string) error {
	insertSQL := fmt.Sprintf(
		`REPLACE INTO %s (sys, modname, sqlid, oldsql, testsql, errmsg, matched, remark)
		 VALUES (?, ?, ?, ?, '', '', '', '')`, c.config.Table)
	_, err := c.db.Exec(insertSQL, sys, modname, sqlid, oldSQL)
	return err
}

// ExplainSQL executes EXPLAIN on a test SQL and returns the result.
func (c *Checker) ExplainSQL(schema, testSQL string) (matched string, errMsg string) {
	// Switch to target schema
	useSQL := fmt.Sprintf("USE %s", schema)
	if _, err := c.db.Exec(useSQL); err != nil {
		// errmsg is VARCHAR(512) under STRICT_TRANS_TABLES: an over-long
		// message makes the whole UPDATE fail, so the row is never written and
		// gets re-fetched on the next run. Truncate like the EXPLAIN path.
		return "F", truncateErr(fmt.Sprintf("cannot switch to schema %s: %s", schema, err.Error()), 512)
	}

	explainSQL := fmt.Sprintf("EXPLAIN %s", testSQL)
	_, err := c.db.Exec(explainSQL)
	if err != nil {
		return "F", truncateErr(err.Error(), 512)
	}
	return "T", ""
}

// UpdateResult updates the matched/errmsg/testsql/suggestion columns after the
// EXPLAIN check. suggestion is the JSON produced by advisor.Analyze for failed
// statements and the empty string for passing ones.
func (c *Checker) UpdateResult(sys, modname, sqlid, testSQL, matched, errMsg, suggestion string) error {
	updateSQL := fmt.Sprintf(
		`UPDATE %s SET matched=?, errmsg=?, testsql=?, suggestion=? WHERE sys=? AND modname=? AND sqlid=?`,
		c.config.Table)
	_, err := c.db.Exec(updateSQL, matched, errMsg, testSQL, suggestion, sys, modname, sqlid)
	return err
}

// GetSQLsToCheck fetches statements that need EXPLAIN verification.
func (c *Checker) GetSQLsToCheck(sys string) ([]*parser.SQLStatement, error) {
	querySQL := fmt.Sprintf(
		`SELECT sys, modname, sqlid, oldsql
		 FROM %s
		 WHERE sys=? AND (matched='' OR matched='F')`, c.config.Table)

	rows, err := c.db.Query(querySQL, sys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stmts []*parser.SQLStatement
	for rows.Next() {
		var s parser.SQLStatement
		if err := rows.Scan(&s.Sys, &s.ModName, &s.SQLID, &s.OldSQL); err != nil {
			return nil, err
		}
		stmts = append(stmts, &s)
	}
	return stmts, rows.Err()
}

// truncateErr truncates an error message to at most maxLen bytes.
//
// The message is returned verbatim — it is stored through a bound parameter
// (`errmsg=?`), so no SQL escaping is needed here. Escaping it would double
// every quote in text that is only ever read back for display.
//
// Two steps keep the result safe to store and render:
//   - invalid bytes are dropped up front, so a message that already arrived
//     corrupted (e.g. a non-utf8mb4 connection charset) still yields valid
//     UTF-8 rather than a half-decoded sequence;
//   - the cut backs off to a rune boundary, so a multi-byte character is never
//     split in half.
//
// The same helper exists in advisor/rules.go (that copy appends an ellipsis);
// keep the two in sync.
func truncateErr(err string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	s := strings.ToValidUTF8(err, "")
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
