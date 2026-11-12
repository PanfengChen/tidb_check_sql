package checker

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"

	_ "github.com/go-sql-driver/mysql"

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

// InitTable creates the result table if it does not exist.
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
	return nil
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
		return "F", fmt.Sprintf("cannot switch to schema %s: %s", schema, err.Error())
	}

	explainSQL := fmt.Sprintf("EXPLAIN %s", testSQL)
	_, err := c.db.Exec(explainSQL)
	if err != nil {
		return "F", truncateErr(err.Error(), 512)
	}
	return "T", ""
}

// UpdateResult updates the matched/errmsg/testsql columns after EXPLAIN check.
func (c *Checker) UpdateResult(sys, modname, sqlid, testSQL, matched, errMsg string) error {
	updateSQL := fmt.Sprintf(
		`UPDATE %s SET matched=?, errmsg=?, testsql=? WHERE sys=? AND modname=? AND sqlid=?`,
		c.config.Table)
	_, err := c.db.Exec(updateSQL, matched, errMsg, testSQL, sys, modname, sqlid)
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

// truncateErr truncates an error message to at most maxLen bytes,
// backing off to a rune boundary so the result stays valid UTF-8.
func truncateErr(err string, maxLen int) string {
	filtered := strings.ReplaceAll(err, "'", "''")
	if len(filtered) <= maxLen {
		return filtered
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(filtered[cut]) {
		cut--
	}
	return filtered[:cut]
}
