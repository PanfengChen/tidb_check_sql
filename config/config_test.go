package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTemp writes content to a temp file and returns its path.
func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApplyDefaults(t *testing.T) {
	c := &Config{}
	c.ApplyDefaults()

	if c.Database.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", c.Database.Host)
	}
	if c.Database.Port != 4000 {
		t.Errorf("Port = %d, want 4000", c.Database.Port)
	}
	if c.Database.User != "root" {
		t.Errorf("User = %q, want root", c.Database.User)
	}
	if c.Database.Schema != "test" {
		t.Errorf("Schema = %q, want test", c.Database.Schema)
	}
	if c.Database.Table != "test.all_sqls" {
		t.Errorf("Table = %q, want test.all_sqls", c.Database.Table)
	}
	if c.Output.Format != "html" {
		t.Errorf("Format = %q, want html", c.Output.Format)
	}
}

// TestApplyDefaultsKeepsExplicitValues guards against defaults clobbering what
// the operator actually set.
func TestApplyDefaultsKeepsExplicitValues(t *testing.T) {
	c := &Config{}
	c.Database.Host = "10.0.0.1"
	c.Database.Port = 4001
	c.Database.User = "bob"
	c.Database.Schema = "orders"
	c.Database.Table = "orders.results"
	c.Output.Format = "json"
	c.ApplyDefaults()

	if c.Database.Host != "10.0.0.1" || c.Database.Port != 4001 || c.Database.User != "bob" {
		t.Errorf("connection settings overwritten: %+v", c.Database)
	}
	if c.Database.Table != "orders.results" {
		t.Errorf("Table = %q, want orders.results unchanged", c.Database.Table)
	}
	if c.Output.Format != "json" {
		t.Errorf("Format = %q, want json", c.Output.Format)
	}
}

// TestNormalizeTableName covers the shape assumption every statement against the
// result table depends on. The DSN sets no default database, so an unqualified
// name fails immediately with 1046 No database selected.
func TestNormalizeTableName(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		table  string
		want   string
	}{
		{"already qualified", "test", "test.all_sqls", "test.all_sqls"},
		{"bare name gets the schema", "orders", "all_sqls", "orders.all_sqls"},
		{"qualified wins over diverging schema", "test", "orders.results", "orders.results"},
		{"empty table falls back to schema prefix", "orders", "", "orders."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{}
			c.Database.Schema = tt.schema
			c.Database.Table = tt.table
			c.NormalizeTableName()
			if c.Database.Table != tt.want {
				t.Fatalf("NormalizeTableName(%q, %q) = %q, want %q",
					tt.schema, tt.table, c.Database.Table, tt.want)
			}
		})
	}
}

// TestApplyCLIOverridesQualifiesTable is the end-to-end version of the same
// guarantee: `-table all_sqls` must come out qualified, not raw.
func TestApplyCLIOverridesQualifiesTable(t *testing.T) {
	c := &Config{}
	c.ApplyDefaults()
	c.ApplyCLIOverrides("", 0, "", "", "orders", "results", "sys1", ".", "")

	if c.Database.Table != "orders.results" {
		t.Fatalf("Table = %q, want orders.results", c.Database.Table)
	}
}

// TestLoadTOMLLegacyFlatKeys pins the documented flat layout.
func TestLoadTOMLLegacyFlatKeys(t *testing.T) {
	p := writeTemp(t, "db.toml", `[database]
host = "10.0.0.1"
port = 4001
user = "alice"
password = "secret"
schema = "orders"
table = "orders.report"

[output]
format = "excel"
verbose = true
`)
	c, err := LoadTOML(p)
	if err != nil {
		t.Fatalf("LoadTOML returned %v", err)
	}
	if c.Database.Host != "10.0.0.1" || c.Database.Port != 4001 || c.Database.User != "alice" {
		t.Errorf("database settings not loaded: %+v", c.Database)
	}
	if c.Database.Table != "orders.report" {
		t.Errorf("Table = %q", c.Database.Table)
	}
	if c.Output.Format != "excel" || !c.Output.Verbose {
		t.Errorf("output settings not loaded: %+v", c.Output)
	}
}

// TestLoadTOMLSystemArray is the guard for the documented multi-system sample.
// The sample in the README used `[check] systems = [...]`, which is a map
// against a slice field and fails to parse; the array-of-tables form below is
// what the struct actually expects.
func TestLoadTOMLSystemArray(t *testing.T) {
	p := writeTemp(t, "multi.toml", `[[check]]
name = "order_sys"
xml_dir = "./xmls/order"
schema = "order_db"

[[check]]
name = "user_sys"
xml_dir = "./xmls/user"
schema = "user_db"
`)
	c, err := LoadTOML(p)
	if err != nil {
		t.Fatalf("array-of-tables form failed to parse: %v", err)
	}
	if len(c.Systems) != 2 {
		t.Fatalf("got %d systems, want 2", len(c.Systems))
	}
	if c.Systems[0].Name != "order_sys" || c.Systems[0].Schema != "order_db" {
		t.Errorf("first system = %+v", c.Systems[0])
	}
	if c.Systems[1].Name != "user_sys" || c.Systems[1].XMLDir != "./xmls/user" {
		t.Errorf("second system = %+v", c.Systems[1])
	}
}

// TestLoadTOMLMapFormFails documents why the old README sample could never
// work: `[check]` with a `systems` key is a table, not an array of tables.
func TestLoadTOMLMapFormFails(t *testing.T) {
	p := writeTemp(t, "bad.toml", `[check]
systems = [
  { name = "order_sys", xml_dir = "./xmls/order", schema = "order_db" },
]
`)
	if _, err := LoadTOML(p); err == nil {
		t.Fatal("expected the map form to fail to parse, but it succeeded")
	}
}

// TestLoadTOMLMissingFile keeps the error path non-silent.
func TestLoadTOMLMissingFile(t *testing.T) {
	if _, err := LoadTOML(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}

// TestLoadTOMLMalformed keeps a syntax error from being swallowed.
func TestLoadTOMLMalformed(t *testing.T) {
	p := writeTemp(t, "broken.toml", "[database\nhost = ")
	if _, err := LoadTOML(p); err == nil {
		t.Fatal("expected an error for malformed TOML")
	}
}

// TestSystemsSurviveDefaultsAndOverrides is the guard for the layer that made
// the documented multi-system config useless even after the TOML syntax was
// fixed: main passed the flag defaults ("default"/".") to ApplyCLIOverrides,
// which replaced every system from the file with one empty one. With only
// explicitly supplied flags forwarded, the list must come through intact.
func TestSystemsSurviveDefaultsAndOverrides(t *testing.T) {
	p := writeTemp(t, "multi.toml", `[[check]]
name = "order_sys"
xml_dir = "./xmls/order"
schema = "order_db"

[[check]]
name = "user_sys"
xml_dir = "./xmls/user"
schema = "user_db"
`)
	c, err := LoadTOML(p)
	if err != nil {
		t.Fatalf("LoadTOML returned %v", err)
	}

	c.ApplyDefaults()
	// No -sys and no -dir from the command line: "" for both.
	c.ApplyCLIOverrides("", 0, "", "", "", "", "", "", "")

	if len(c.Systems) != 2 {
		t.Fatalf("got %d systems after defaults+overrides, want 2: %+v", len(c.Systems), c.Systems)
	}
	if c.Systems[0].Name != "order_sys" || c.Systems[0].XMLDir != "./xmls/order" ||
		c.Systems[0].Schema != "order_db" {
		t.Errorf("first system mangled: %+v", c.Systems[0])
	}
	if c.Systems[1].Name != "user_sys" || c.Systems[1].XMLDir != "./xmls/user" ||
		c.Systems[1].Schema != "user_db" {
		t.Errorf("second system mangled: %+v", c.Systems[1])
	}
}

// TestExplicitSysReplacesSystems documents the intended opposite case: naming a
// system on the command line is an explicit narrowing and does replace the list.
func TestExplicitSysReplacesSystems(t *testing.T) {
	p := writeTemp(t, "multi.toml", `[[check]]
name = "order_sys"
xml_dir = "./xmls/order"
schema = "order_db"

[[check]]
name = "user_sys"
xml_dir = "./xmls/user"
schema = "user_db"
`)
	c, err := LoadTOML(p)
	if err != nil {
		t.Fatalf("LoadTOML returned %v", err)
	}
	c.ApplyDefaults()
	c.ApplyCLIOverrides("", 0, "", "", "", "", "only_this", "./other", "")

	if len(c.Systems) != 1 {
		t.Fatalf("got %d systems, want 1 after an explicit -sys/-dir", len(c.Systems))
	}
	if c.Systems[0].Name != "only_this" || c.Systems[0].XMLDir != "./other" {
		t.Fatalf("explicit override not applied: %+v", c.Systems[0])
	}
}

// TestApplyDefaultsCreatesOneSystem guards the no-config path: something has to
// exist for main to read Systems[0] from.
func TestApplyDefaultsCreatesOneSystem(t *testing.T) {
	c := &Config{}
	c.ApplyDefaults()

	if len(c.Systems) != 1 {
		t.Fatalf("got %d systems, want 1", len(c.Systems))
	}
	if c.Systems[0].Name != "default" || c.Systems[0].XMLDir != "." {
		t.Fatalf("default system = %+v", c.Systems[0])
	}
}
