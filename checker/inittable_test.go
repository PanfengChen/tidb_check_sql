package checker

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"sync"
	"testing"
)

// fakeDB records statements issued through database/sql and returns a canned
// row count for the information_schema probe, so InitTable's pre-check can be
// verified without a live TiDB.
type fakeDB struct {
	mu      sync.Mutex
	count   int64
	queries []string
	args    [][]driver.Value
	execs   []string
	// execConn records which connection handle served each exec, so tests can
	// assert session state (USE) and the statement that depends on it share one
	// connection. connSeq hands out those ids.
	execConn []int
	connSeq  int
}

func (f *fakeDB) nextConnID() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connSeq++
	return f.connSeq
}

func (f *fakeDB) recordQuery(q string, a []driver.NamedValue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	vals := make([]driver.Value, len(a))
	for i, nv := range a {
		vals[i] = nv.Value
	}
	f.args = append(f.args, vals)
}

func (f *fakeDB) recordExec(q string, connID int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, q)
	f.execConn = append(f.execConn, connID)
}

type fakeConnector struct{ db *fakeDB }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) {
	return &fakeConn{db: c.db, id: c.db.nextConnID()}, nil
}
func (c fakeConnector) Driver() driver.Driver { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, io.EOF }

type fakeConn struct {
	db *fakeDB
	id int
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)           { return nil, io.EOF }

func (c *fakeConn) QueryContext(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	c.db.recordQuery(q, a)
	return &fakeRows{val: c.db.count}, nil
}

func (c *fakeConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	c.db.recordExec(q, c.id)
	return driver.RowsAffected(0), nil
}

type fakeRows struct {
	val  int64
	done bool
}

func (r *fakeRows) Columns() []string { return []string{"count"} }
func (r *fakeRows) Close() error      { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.val
	return nil
}

func newFakeChecker(t *testing.T, columnExists bool) (*Checker, *fakeDB) {
	t.Helper()
	f := &fakeDB{count: 0}
	if columnExists {
		f.count = 1
	}
	db := sql.OpenDB(fakeConnector{db: f})
	t.Cleanup(func() { db.Close() })
	return &Checker{db: db, config: DBConfig{Schema: "test", Table: "test.all_sqls"}}, f
}

// TestInitTableSkipsAlterWhenColumnExists is the regression guard for the
// schema pre-check: with the column present, InitTable must not issue ALTER.
// An unconditional ALTER required ALTER privileges on every start, which broke
// accounts holding only CREATE/INSERT/UPDATE (error 1142).
func TestInitTableSkipsAlterWhenColumnExists(t *testing.T) {
	c, f := newFakeChecker(t, true)

	if err := c.InitTable(); err != nil {
		t.Fatalf("InitTable returned %v", err)
	}

	for _, q := range f.execs {
		if strings.Contains(strings.ToUpper(q), "ALTER TABLE") {
			t.Fatalf("InitTable issued ALTER on a table that already has the column: %s", q)
		}
	}
	if len(f.execs) == 0 || !strings.Contains(strings.ToUpper(f.execs[0]), "CREATE TABLE") {
		t.Fatalf("expected CREATE TABLE first, got %v", f.execs)
	}
}

// TestInitTableAlterProbeUsesBareTableName pins the two facts that made the
// original probe match nothing: table_name must be the bare identifier, and the
// schema must come from configuration because the DSN sets no default database
// (so DATABASE() is NULL for the whole of InitTable).
func TestInitTableAlterProbeUsesBareTableName(t *testing.T) {
	c, f := newFakeChecker(t, true)

	if err := c.InitTable(); err != nil {
		t.Fatalf("InitTable returned %v", err)
	}
	if len(f.queries) != 1 {
		t.Fatalf("expected exactly 1 information_schema query, got %d: %v", len(f.queries), f.queries)
	}

	q := f.queries[0]
	if strings.Contains(strings.ToUpper(q), "DATABASE()") {
		t.Fatalf("probe relies on DATABASE(), which is NULL without a default database: %s", q)
	}

	args := f.args[0]
	want := []driver.Value{"test", "all_sqls", "suggestion"}
	if len(args) != len(want) {
		t.Fatalf("probe args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("probe args = %v, want %v", args, want)
		}
	}
}

func TestInitTableAddsColumnWhenMissing(t *testing.T) {
	c, f := newFakeChecker(t, false)

	if err := c.InitTable(); err != nil {
		t.Fatalf("InitTable returned %v", err)
	}

	found := false
	for _, q := range f.execs {
		if strings.Contains(strings.ToUpper(q), "ADD COLUMN SUGGESTION") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ALTER adding the suggestion column, got %v", f.execs)
	}
}

// TestExplainSQLPinsOneConnection is the regression guard for the USE/EXPLAIN
// split: they were two separate db.Exec calls, so the pool could serve the
// EXPLAIN from a different connection — a fresh one with no default database
// (1046) or, in a multi-system run, one still pointing at the previous schema.
// Both statements must now go over the same connection handle.
func TestExplainSQLPinsOneConnection(t *testing.T) {
	c, f := newFakeChecker(t, true)

	matched, errMsg := c.ExplainSQL("order_db", "select 1 from t")
	if matched != "T" || errMsg != "" {
		t.Fatalf("ExplainSQL = (%q, %q), want (T, \"\")", matched, errMsg)
	}

	if len(f.execs) != 2 {
		t.Fatalf("expected USE + EXPLAIN, got %v", f.execs)
	}
	if !strings.HasPrefix(strings.ToUpper(f.execs[0]), "USE ORDER_DB") {
		t.Fatalf("first exec should switch schema, got %q", f.execs[0])
	}
	if !strings.HasPrefix(strings.ToUpper(f.execs[1]), "EXPLAIN") {
		t.Fatalf("second exec should be EXPLAIN, got %q", f.execs[1])
	}
	if f.execConn[0] != f.execConn[1] {
		t.Fatalf("USE and EXPLAIN ran on different connections (%d vs %d); "+
			"session state would not apply", f.execConn[0], f.execConn[1])
	}
}

// TestExplainSQLReissuesUseEachCall guards the other half: the pinned
// connection is recycled, so every call must set the schema rather than rely on
// whatever the previous call left behind.
func TestExplainSQLReissuesUseEachCall(t *testing.T) {
	c, f := newFakeChecker(t, true)

	if matched, _ := c.ExplainSQL("schema_a", "select 1"); matched != "T" {
		t.Fatalf("first ExplainSQL failed")
	}
	if matched, _ := c.ExplainSQL("schema_b", "select 2"); matched != "T" {
		t.Fatalf("second ExplainSQL failed")
	}

	if len(f.execs) != 4 {
		t.Fatalf("expected 2 x (USE + EXPLAIN), got %v", f.execs)
	}
	for _, i := range []int{0, 2} {
		if !strings.HasPrefix(strings.ToUpper(f.execs[i]), "USE ") {
			t.Fatalf("exec %d should be USE, got %q", i, f.execs[i])
		}
	}
	if !strings.Contains(strings.ToUpper(f.execs[2]), "SCHEMA_B") {
		t.Fatalf("second call must switch to schema_b, got %q", f.execs[2])
	}
}

// TestExplainSQLConnectionErrorIsReported keeps the failure path honest: if no
// connection can be acquired the row must be marked F with a message, not
// silently passed.
func TestExplainSQLConnectionErrorIsReported(t *testing.T) {
	db := sql.OpenDB(errConnector{})
	t.Cleanup(func() { db.Close() })
	c := &Checker{db: db, config: DBConfig{Schema: "test", Table: "test.all_sqls"}}

	matched, errMsg := c.ExplainSQL("test", "select 1")
	if matched != "F" {
		t.Fatalf("matched = %q, want F when the connection cannot be acquired", matched)
	}
	if errMsg == "" {
		t.Fatal("expected a non-empty error message")
	}
	if len(errMsg) > 512 {
		t.Fatalf("error message is %d bytes, must fit VARCHAR(512)", len(errMsg))
	}
}

type errConnector struct{}

func (errConnector) Connect(context.Context) (driver.Conn, error) { return nil, io.EOF }
func (errConnector) Driver() driver.Driver                        { return fakeDriver{} }
