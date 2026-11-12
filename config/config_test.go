package config

import (
	"os"
	"path/filepath"
	"strings"
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
		{"empty table is left alone", "orders", "", ""},
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
// guarantee: `-table results` must come out qualified, not raw.
//
// The error return is checked on purpose. Normalisation now runs before the
// system is resolved, so Table is qualified even when the call fails; without
// this assertion the test would pass for a call that was rejected outright and
// would only prove "the table name gets prefixed", not "this valid input
// succeeds".
func TestApplyCLIOverridesQualifiesTable(t *testing.T) {
	c := &Config{}
	c.ApplyDefaults()
	if err := c.ApplyCLIOverrides("", 0, "", "", "orders", "results", "sys1", ".", ""); err != nil {
		t.Fatalf("ApplyCLIOverrides returned %v", err)
	}

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

// TestExplicitSysNarrowsToDeclaredSystem replaces the old
// TestExplicitSysReplacesSystems. That test passed -sys with a name absent from
// the config and asserted the list was rebuilt, which pinned the wrong
// semantics: narrating the error text's own advice ("narrow with -sys") then
// produced a system with no xml_dir and no schema, so EXPLAIN ran against the
// wrong database. Naming a *declared* system must narrow to it and keep
// everything the flags do not override.
func TestExplicitSysNarrowsToDeclaredSystem(t *testing.T) {
	newMultiConfig := func() *Config {
		return &Config{
			Database: DBConfig{Schema: "test", Table: "test.all_sqls"},
			Systems: []SystemConfig{
				{Name: "order_sys", XMLDir: "./xmls/order", Schema: "order_db"},
				{Name: "user_sys", XMLDir: "./xmls/user", Schema: "user_db"},
			},
		}
	}

	t.Run("sys alone keeps dir and schema", func(t *testing.T) {
		c := newMultiConfig()
		if err := c.ApplyCLIOverrides("", 0, "", "", "", "", "user_sys", "", ""); err != nil {
			t.Fatalf("ApplyCLIOverrides returned %v", err)
		}
		if len(c.Systems) != 1 {
			t.Fatalf("got %d systems, want 1", len(c.Systems))
		}
		want := SystemConfig{Name: "user_sys", XMLDir: "./xmls/user", Schema: "user_db"}
		if c.Systems[0] != want {
			t.Fatalf("system = %+v, want %+v", c.Systems[0], want)
		}
		if got := ResolveTargetSchema(c.Systems[0].Schema, c.Database.Schema); got != "user_db" {
			t.Fatalf("targetSchema = %q, want user_db", got)
		}
	})

	t.Run("sys plus dir and schema merges", func(t *testing.T) {
		c := newMultiConfig()
		// Overriding only the schema must leave the declared xml_dir in place;
		// a rebuild would drop it back to ".".
		if err := c.ApplyCLIOverrides("", 0, "", "", "cli_db", "", "order_sys", "", ""); err != nil {
			t.Fatalf("ApplyCLIOverrides returned %v", err)
		}
		want := SystemConfig{Name: "order_sys", XMLDir: "./xmls/order", Schema: "cli_db"}
		if len(c.Systems) != 1 || c.Systems[0] != want {
			t.Fatalf("systems = %+v, want [%+v] (xml_dir must survive)", c.Systems, want)
		}

		c2 := newMultiConfig()
		if err := c2.ApplyCLIOverrides("", 0, "", "", "cli_db", "", "order_sys", "./other", ""); err != nil {
			t.Fatalf("ApplyCLIOverrides returned %v", err)
		}
		want2 := SystemConfig{Name: "order_sys", XMLDir: "./other", Schema: "cli_db"}
		if len(c2.Systems) != 1 || c2.Systems[0] != want2 {
			t.Fatalf("systems = %+v, want [%+v]", c2.Systems, want2)
		}
	})

	t.Run("unknown sys is an error", func(t *testing.T) {
		c := newMultiConfig()
		err := c.ApplyCLIOverrides("", 0, "", "", "", "", "nope", "./x", "")
		if err == nil {
			t.Fatal("expected an error for a system name absent from the config")
		}
		if !strings.Contains(err.Error(), "order_sys") || !strings.Contains(err.Error(), "user_sys") {
			t.Fatalf("error should list the available systems, got %v", err)
		}
	})

	t.Run("dir without sys is an error", func(t *testing.T) {
		c := newMultiConfig()
		err := c.ApplyCLIOverrides("", 0, "", "", "", "", "", "./xmls/user", "")
		if err == nil {
			t.Fatal("expected an error: -dir alone cannot say which system it applies to")
		}
		if len(c.Systems) != 2 {
			t.Fatalf("systems must be left untouched on error, got %+v", c.Systems)
		}
	})

	t.Run("schema without sys is an error", func(t *testing.T) {
		c := newMultiConfig()
		if err := c.ApplyCLIOverrides("", 0, "", "", "other_db", "", "", "", ""); err == nil {
			t.Fatal("expected an error: -schema alone cannot say which system it applies to")
		}
	})
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

// TestPartialFlagsMergeIntoSystem is the guard for the silent-wrong-database
// bug: passing one flag used to rebuild the system struct, dropping the fields
// the user did not repeat. `-dir ./other` alone cleared the configured schema,
// so EXPLAIN ran against Database.Schema instead of the system's schema.
func TestPartialFlagsMergeIntoSystem(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		sys        string
		dir        string
		wantName   string
		wantDir    string
		wantSchema string
	}{
		{
			name: "dir only keeps name and schema", dir: "./other",
			wantName: "order_sys", wantDir: "./other", wantSchema: "order_db",
		},
		{
			// Naming the declared system is a no-op on the name and must still
			// keep the fields the user did not pass.
			name: "sys matches declared name", sys: "order_sys",
			wantName: "order_sys", wantDir: "./xmls/order", wantSchema: "order_db",
		},
		{
			name: "schema only keeps name and dir", schema: "other_db",
			wantName: "order_sys", wantDir: "./xmls/order", wantSchema: "other_db",
		},
		{
			name: "dir and schema with matching sys", sys: "order_sys", dir: "./d2", schema: "db2",
			wantName: "order_sys", wantDir: "./d2", wantSchema: "db2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{}
			c.Database.Schema = "test"
			c.Database.Table = "test.all_sqls"
			c.Systems = []SystemConfig{{Name: "order_sys", XMLDir: "./xmls/order", Schema: "order_db"}}

			c.ApplyCLIOverrides("", 0, "", "", tt.schema, "", tt.sys, tt.dir, "")

			if len(c.Systems) != 1 {
				t.Fatalf("got %d systems, want 1", len(c.Systems))
			}
			sys := c.Systems[0]
			if sys.Name != tt.wantName || sys.XMLDir != tt.wantDir || sys.Schema != tt.wantSchema {
				t.Fatalf("system = %+v, want {Name:%q XMLDir:%q Schema:%q}",
					sys, tt.wantName, tt.wantDir, tt.wantSchema)
			}
		})
	}
}

// TestSchemaFlagWinsForTargetSchema pins the documented precedence: a -schema on
// the command line must beat the schema declared in the config file. Previously
// the flag only set Database.Schema while targetSchema preferred the (unchanged)
// system schema, so -schema had no effect at all.
func TestSchemaFlagWinsForTargetSchema(t *testing.T) {
	c := &Config{}
	c.Database.Schema = "test"
	c.Systems = []SystemConfig{{Name: "order_sys", XMLDir: "./xmls/order", Schema: "order_db"}}

	c.ApplyCLIOverrides("", 0, "", "", "other_db", "", "", "", "")

	if got := ResolveTargetSchema(c.Systems[0].Schema, c.Database.Schema); got != "other_db" {
		t.Fatalf("targetSchema = %q, want other_db", got)
	}
}

func TestResolveTargetSchema(t *testing.T) {
	tests := []struct {
		name         string
		systemSchema string
		dbSchema     string
		want         string
	}{
		{"system schema wins", "order_db", "test", "order_db"},
		{"falls back to db schema", "", "test", "test"},
		{"both empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveTargetSchema(tt.systemSchema, tt.dbSchema); got != tt.want {
				t.Fatalf("ResolveTargetSchema(%q, %q) = %q, want %q",
					tt.systemSchema, tt.dbSchema, got, tt.want)
			}
		})
	}
}

// TestNormalizeTableNameEmptySchemaDoesNotProduceLeadingDot guards the exported
// method against a partially filled Config: an empty schema must not yield
// ".all_sqls", which is not a valid identifier.
func TestNormalizeTableNameEmptySchemaDoesNotProduceLeadingDot(t *testing.T) {
	c := &Config{}
	c.Database.Table = "all_sqls"
	c.NormalizeTableName()

	if c.Database.Table != "all_sqls" {
		t.Fatalf("Table = %q, want it left alone when the schema is empty", c.Database.Table)
	}
	if strings.HasPrefix(c.Database.Table, ".") {
		t.Fatalf("Table = %q must not start with a dot", c.Database.Table)
	}
}

// TestUnknownSysOnSingleSystemIsAnError pins the consistency the multi-system
// branch already had. Relabelling a declared system to an undeclared name used
// to succeed silently, which left the result table's sys column disagreeing with
// the config and made historical rows unfindable.
func TestUnknownSysOnSingleSystemIsAnError(t *testing.T) {
	c := &Config{}
	c.Database.Schema = "test"
	c.Systems = []SystemConfig{{Name: "order_sys", XMLDir: "./xmls/order", Schema: "order_db"}}

	err := c.ApplyCLIOverrides("", 0, "", "", "", "", "not_declared", "", "")
	if err == nil {
		t.Fatal("expected an error for an undeclared -sys on a single-system config")
	}
	if c.Systems[0].Name != "order_sys" {
		t.Fatalf("system name was changed on error: %+v", c.Systems[0])
	}
}

// TestRenamePlaceholderSystemIsAllowed: with no config file the system is the
// synthetic "default" placeholder, so naming it is a legitimate rename rather
// than a typo.
func TestRenamePlaceholderSystemIsAllowed(t *testing.T) {
	c := &Config{}
	c.Database.Schema = "test"
	c.Database.Table = "test.all_sqls"
	c.ApplyDefaults()
	c.ApplyCLIOverrides("", 0, "", "", "", "", "my_sys", "./xml", "")

	if err := c.ApplyCLIOverrides("", 0, "", "", "", "", "my_sys", "", ""); err != nil {
		t.Fatalf("naming the placeholder system should work, got %v", err)
	}
	if c.Systems[0].Name != "my_sys" {
		t.Fatalf("system = %+v", c.Systems[0])
	}
}

// TestLoadTOMLRejectsDuplicateSystemNames: -sys narrows by name and keeps the
// first match, so duplicates would silently drop the rest.
func TestLoadTOMLRejectsDuplicateSystemNames(t *testing.T) {
	p := writeTemp(t, "dup.toml", `[[check]]
name = "dup"
xml_dir = "./a"
schema = "db_a"

[[check]]
name = "dup"
xml_dir = "./b"
schema = "db_b"
`)
	_, err := LoadTOML(p)
	if err == nil {
		t.Fatal("expected an error for duplicate system names")
	}
	if !strings.Contains(err.Error(), "dup") {
		t.Fatalf("error should name the duplicated system, got %v", err)
	}
}

// TestLoadTOMLRejectsUnnamedSystem: an unnamed [[check]] is almost certainly a
// typo, and the duplicate-name message would otherwise read "system """.
func TestLoadTOMLRejectsUnnamedSystem(t *testing.T) {
	p := writeTemp(t, "noname.toml", `[[check]]
xml_dir = "./a"
schema = "db_a"
`)
	_, err := LoadTOML(p)
	if err == nil {
		t.Fatal("expected an error for a [[check]] without a name")
	}
	if !strings.Contains(err.Error(), "no name") {
		t.Fatalf("error should say the system has no name, got %v", err)
	}
}

// TestDeclaredSystemNamedDefaultIsNotARename: the placeholder exemption keys on
// provenance, not on the literal name, because a config may legitimately declare
// a system called "default". Comparing names let a typo through silently.
func TestDeclaredSystemNamedDefaultIsNotARename(t *testing.T) {
	c := &Config{}
	c.Database.Schema = "test"
	c.Systems = []SystemConfig{{Name: "default", XMLDir: "./xmls/real", Schema: "real_db"}}

	err := c.ApplyCLIOverrides("", 0, "", "", "", "", "typo_sys", "", "")
	if err == nil {
		t.Fatal("expected an error: a declared system named \"default\" must not be renamed silently")
	}
	if c.Systems[0].Name != "default" || c.Systems[0].XMLDir != "./xmls/real" {
		t.Fatalf("config was mutated on error: %+v", c.Systems[0])
	}
}

// TestDeclaredDefaultViaTOMLLoadIsAlsoProtected is the same guarantee through the
// real loading path, where the provenance flag is set by LoadTOML.
func TestDeclaredDefaultViaTOMLLoadIsAlsoProtected(t *testing.T) {
	p := writeTemp(t, "real.toml", `[[check]]
name = "default"
xml_dir = "./xmls/real"
schema = "real_db"
`)
	c, err := LoadTOML(p)
	if err != nil {
		t.Fatalf("LoadTOML returned %v", err)
	}
	c.ApplyDefaults()

	if err := c.ApplyCLIOverrides("", 0, "", "", "", "", "typo", "", ""); err == nil {
		t.Fatal("expected an error for renaming a system declared in the config file")
	}
}

// TestConfigWithoutCheckSectionAllowsRenaming: a config file that sets only
// [database] declares no system, so ApplyDefaults creates the placeholder and
// -sys is free to name it. The README used to scope this to "no config file",
// which understated it.
func TestConfigWithoutCheckSectionAllowsRenaming(t *testing.T) {
	p := writeTemp(t, "dbonly.toml", `[database]
host = "10.0.0.1"
schema = "realdb"
`)
	c, err := LoadTOML(p)
	if err != nil {
		t.Fatalf("LoadTOML returned %v", err)
	}
	c.ApplyDefaults()

	if err := c.ApplyCLIOverrides("", 0, "", "", "", "", "anything", "", ""); err != nil {
		t.Fatalf("expected the placeholder to be renamable, got %v", err)
	}
	if c.Systems[0].Name != "anything" {
		t.Fatalf("system = %+v, want the requested name", c.Systems[0])
	}
}

// TestDefaultSystemNameIsMaterialized: without -sys the placeholder must still
// reach the result table with a usable name, never an empty string.
func TestDefaultSystemNameIsMaterialized(t *testing.T) {
	c := &Config{}
	c.ApplyDefaults()
	if err := c.ApplyCLIOverrides("", 0, "", "", "", "", "", "", ""); err != nil {
		t.Fatalf("ApplyCLIOverrides returned %v", err)
	}

	if c.Systems[0].Name != "default" {
		t.Fatalf("system name = %q, want \"default\"", c.Systems[0].Name)
	}
}

// TestPlaceholderFromOverridesIsAlsoRenamable: the same placeholder must behave
// the same no matter which function created it. applySystemOverrides could build
// one itself without carrying the provenance mark, so a Config that skipped
// ApplyDefaults saw its own synthetic system rejected as "not declared".
func TestPlaceholderFromOverridesIsAlsoRenamable(t *testing.T) {
	c := &Config{}
	c.Database.Schema = "test"
	c.Database.Table = "test.all_sqls"

	if err := c.ApplyCLIOverrides("", 0, "", "", "", "", "mysys", "", ""); err != nil {
		t.Fatalf("placeholder built by ApplyCLIOverrides should be renamable, got %v", err)
	}
	if len(c.Systems) != 1 || c.Systems[0].Name != "mysys" {
		t.Fatalf("systems = %+v, want one system named mysys", c.Systems)
	}
}

// TestEnsureSyntheticSystemIsIdempotent: the helper must not clobber a config
// that already has systems. Keeping the guard inside makes it safe for a caller
// that forgets the len==0 check — otherwise it would reproduce the very bug this
// round fixed, silently replacing every declared system with the placeholder.
func TestEnsureSyntheticSystemIsIdempotent(t *testing.T) {
	c := &Config{}
	c.Systems = []SystemConfig{
		{Name: "order_sys", XMLDir: "./xmls/order", Schema: "order_db"},
		{Name: "user_sys", XMLDir: "./xmls/user", Schema: "user_db"},
	}

	c.ensureSyntheticSystem()

	if len(c.Systems) != 2 {
		t.Fatalf("got %d systems, want the declared 2 left untouched", len(c.Systems))
	}
	if c.Systems[0].Name != "order_sys" || c.Systems[1].Name != "user_sys" {
		t.Fatalf("systems were replaced: %+v", c.Systems)
	}
	if c.syntheticSystem {
		t.Fatal("syntheticSystem must not be set when real systems exist")
	}
}

// TestEnsureSyntheticSystemInstallsPlaceholder covers the empty case it exists
// for.
func TestEnsureSyntheticSystemInstallsPlaceholder(t *testing.T) {
	c := &Config{}
	c.ensureSyntheticSystem()

	if len(c.Systems) != 1 || c.Systems[0].Name != "default" || c.Systems[0].XMLDir != "." {
		t.Fatalf("placeholder = %+v", c.Systems)
	}
	if !c.syntheticSystem {
		t.Fatal("syntheticSystem must be set for the placeholder")
	}
}

// TestTableQualifiedEvenWhenOverridesFail: normalisation runs before the system
// is resolved, so a rejected -sys still leaves Database.Table qualified. Leaving
// it bare would make CREATE TABLE fail with 1046 for any caller that decides to
// carry on after the error — resurrecting a bug fixed two rounds ago.
func TestTableQualifiedEvenWhenOverridesFail(t *testing.T) {
	c := &Config{}
	c.Database.Schema = "test"
	c.Database.Table = "all_sqls"
	c.Systems = []SystemConfig{{Name: "real", XMLDir: "./xmls/real", Schema: "real_db"}}

	if err := c.ApplyCLIOverrides("", 0, "", "", "", "", "typo_sys", "", ""); err == nil {
		t.Fatal("expected an error for an undeclared system name")
	}
	if c.Database.Table != "test.all_sqls" {
		t.Fatalf("Table = %q after a failed override, want it qualified", c.Database.Table)
	}
}

// TestEmptyFlagsDoNotClobberConfig is the counterpart to "flags win": a flag the
// user did not pass arrives as its zero value and must leave the config alone.
// These branches were previously only walked indirectly, so a regression that
// treated 0/"" as "clear this field" would not have been caught.
func TestEmptyFlagsDoNotClobberConfig(t *testing.T) {
	c := &Config{}
	c.Database = DBConfig{
		Host:     "10.0.0.1",
		Port:     4001,
		User:     "alice",
		Password: "secret",
		Schema:   "orders",
		Table:    "orders.report",
	}
	c.Output.Format = "json"
	c.Systems = []SystemConfig{{Name: "order_sys", XMLDir: "./xmls/order", Schema: "order_db"}}

	// Every flag at its zero value: nothing was supplied on the command line.
	if err := c.ApplyCLIOverrides("", 0, "", "", "", "", "", "", ""); err != nil {
		t.Fatalf("ApplyCLIOverrides returned %v", err)
	}

	if c.Database.Host != "10.0.0.1" || c.Database.Port != 4001 {
		t.Fatalf("connection settings clobbered: %+v", c.Database)
	}
	if c.Database.User != "alice" || c.Database.Password != "secret" {
		t.Fatalf("credentials clobbered: user=%q", c.Database.User)
	}
	if c.Database.Schema != "orders" || c.Database.Table != "orders.report" {
		t.Fatalf("schema/table clobbered: %+v", c.Database)
	}
	if c.Output.Format != "json" {
		t.Fatalf("output format clobbered: %q", c.Output.Format)
	}
	if c.Systems[0].Name != "order_sys" || c.Systems[0].XMLDir != "./xmls/order" {
		t.Fatalf("system clobbered: %+v", c.Systems[0])
	}
}

// TestEachFlagAppliedIndividually walks the per-field branches so a mistake in
// any single one is caught with a precise message.
func TestEachFlagAppliedIndividually(t *testing.T) {
	tests := []struct {
		name   string
		apply  func(c *Config) error
		assert func(t *testing.T, c *Config)
	}{
		{
			name:  "host",
			apply: func(c *Config) error { return c.ApplyCLIOverrides("db.example", 0, "", "", "", "", "", "", "") },
			assert: func(t *testing.T, c *Config) {
				if c.Database.Host != "db.example" {
					t.Fatalf("Host = %q", c.Database.Host)
				}
			},
		},
		{
			name:  "port",
			apply: func(c *Config) error { return c.ApplyCLIOverrides("", 4002, "", "", "", "", "", "", "") },
			assert: func(t *testing.T, c *Config) {
				if c.Database.Port != 4002 {
					t.Fatalf("Port = %d", c.Database.Port)
				}
			},
		},
		{
			// Split from "password" deliberately: a cross-wired assignment
			// (user written into password) must name the field it broke, which
			// a combined assertion cannot do.
			name:  "user",
			apply: func(c *Config) error { return c.ApplyCLIOverrides("", 0, "bob", "", "", "", "", "", "") },
			assert: func(t *testing.T, c *Config) {
				if c.Database.User != "bob" {
					t.Fatalf("User = %q, want bob", c.Database.User)
				}
			},
		},
		{
			name:  "password",
			apply: func(c *Config) error { return c.ApplyCLIOverrides("", 0, "", "pw2", "", "", "", "", "") },
			assert: func(t *testing.T, c *Config) {
				if c.Database.Password != "pw2" {
					t.Fatalf("Password = %q, want pw2", c.Database.Password)
				}
			},
		},
		{
			name:  "schema",
			apply: func(c *Config) error { return c.ApplyCLIOverrides("", 0, "", "", "newdb", "", "", "", "") },
			assert: func(t *testing.T, c *Config) {
				if c.Database.Schema != "newdb" {
					t.Fatalf("Schema = %q", c.Database.Schema)
				}
			},
		},
		{
			name:  "output",
			apply: func(c *Config) error { return c.ApplyCLIOverrides("", 0, "", "", "", "", "", "", "csv") },
			assert: func(t *testing.T, c *Config) {
				if c.Output.Format != "csv" {
					t.Fatalf("Format = %q", c.Output.Format)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{}
			c.Database = DBConfig{Host: "orig", Port: 4000, User: "orig", Schema: "test", Table: "test.all_sqls"}
			c.Output.Format = "html"
			c.Systems = []SystemConfig{{Name: "default", XMLDir: "."}}

			if err := tt.apply(c); err != nil {
				t.Fatalf("ApplyCLIOverrides returned %v", err)
			}
			tt.assert(t, c)
		})
	}
}
