package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// DBConfig holds database connection parameters.
type DBConfig struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	Schema   string `toml:"schema"`
	Table    string `toml:"table"`
}

// SystemConfig maps a system name to its XML directory and target schema.
type SystemConfig struct {
	Name   string `toml:"name"`
	XMLDir string `toml:"xml_dir"`
	Schema string `toml:"schema"`
}

// OutputConfig holds output formatting options.
type OutputConfig struct {
	Format  string `toml:"format"` // html, json, csv, excel, terminal
	Verbose bool   `toml:"verbose"`
}

// Config is the top-level configuration structure.
type Config struct {
	Database DBConfig       `toml:"database"`
	Systems  []SystemConfig `toml:"check"`
	Output   OutputConfig   `toml:"output"`

	// syntheticSystem records that Systems was filled in by ApplyDefaults
	// rather than read from a config file. Renaming that placeholder with -sys
	// is legitimate; renaming a declared system is a typo. Deciding by name
	// would misfire on a config that happens to declare a system called
	// "default".
	syntheticSystem bool
}

// LoadTOML reads a TOML configuration file.
func LoadTOML(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read config file %s: %w", path, err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("cannot parse config file %s: %w", path, err)
	}
	// Duplicate names are meaningless and dangerous: -sys narrows by name and
	// would silently keep the first match, dropping the rest.
	seen := make(map[string]bool, len(cfg.Systems))
	for i, s := range cfg.Systems {
		if s.Name == "" {
			return nil, fmt.Errorf("config file %s: system #%d has no name; every [[check]] needs one", path, i+1)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("config file %s declares system %q more than once", path, s.Name)
		}
		seen[s.Name] = true
	}
	return &cfg, nil
}

// ApplyDefaults fills in default values for optional fields.
func (c *Config) ApplyDefaults() {
	if c.Database.Host == "" {
		c.Database.Host = "127.0.0.1"
	}
	if c.Database.Port == 0 {
		c.Database.Port = 4000
	}
	if c.Database.User == "" {
		c.Database.User = "root"
	}
	if c.Database.Schema == "" {
		c.Database.Schema = "test"
	}
	if c.Database.Table == "" {
		c.Database.Table = "test.all_sqls"
	}
	if c.Output.Format == "" {
		c.Output.Format = "html"
	}
	if len(c.Systems) == 0 {
		c.ensureSyntheticSystem()
	}
	c.NormalizeTableName()
}

// NormalizeTableName makes Database.Table schema-qualified.
//
// Every statement the tool issues against the result table (CREATE, REPLACE,
// UPDATE, and the information_schema probe) reads Database.Table, and the DSN
// carries no default database — so an unqualified name such as "all_sqls"
// fails immediately with 1046 No database selected. Qualifying once, here,
// keeps those call sites on the same assumption instead of each guessing.
func (c *Config) NormalizeTableName() {
	// Guard against producing ".table": ApplyDefaults guarantees a schema, but
	// this is exported and may be called on a partially filled Config.
	if c.Database.Schema == "" || c.Database.Table == "" {
		return
	}
	if !strings.Contains(c.Database.Table, ".") {
		c.Database.Table = c.Database.Schema + "." + c.Database.Table
	}
}

// ApplyCLIOverrides applies command-line flags on top of the loaded config.
//
// Destructive: narrowing trims Systems down to the single system this run will
// check, so the original list cannot be recovered. Call it once per loaded
// Config; a second call would treat the already-narrowed list as the full
// configuration.
//
// On error the Config may already be partially updated: Database fields are
// applied first, and Database.Table is normalised before the system is resolved.
// Callers should treat a non-nil error as "this Config is unusable" rather than
// continuing with it.
func (c *Config) ApplyCLIOverrides(host string, port int, user, password, schema, table, sys, dir, output string) error {
	if host != "" {
		c.Database.Host = host
	}
	if port > 0 {
		c.Database.Port = port
	}
	if user != "" {
		c.Database.User = user
	}
	if password != "" {
		c.Database.Password = password
	}
	if schema != "" {
		c.Database.Schema = schema
	}
	if table != "" {
		c.Database.Table = table
	}
	if output != "" {
		c.Output.Format = output
	}
	// Normalise before resolving the system, not after: it is idempotent and only
	// adds the schema prefix, so running it first costs nothing — and it keeps the
	// table qualified even when the system resolution below fails, instead of
	// leaving a bare "all_sqls" that CREATE TABLE would reject with 1046 if a
	// caller ever decides to continue past the error.
	c.NormalizeTableName()

	// Only flags the user actually supplied reach here ("" means "not
	// specified"); main must never forward flag defaults.
	return c.applySystemOverrides(sys, dir, schema)
}

// ensureSyntheticSystem installs the default placeholder system and records its
// provenance. Every placeholder must be created here: the rename check relies on
// syntheticSystem, so a placeholder built anywhere else would be rejected as an
// unknown declared name, making an identical system renamable or not depending on
// which function happened to create it.
// It is idempotent: when the config already declares systems it does nothing at
// all — it neither replaces them nor marks them synthetic. That way a caller who
// forgets the len==0 guard cannot silently replace the user's configuration with
// the placeholder.
func (c *Config) ensureSyntheticSystem() {
	if len(c.Systems) > 0 {
		return
	}
	c.Systems = []SystemConfig{{Name: "default", XMLDir: "."}}
	c.syntheticSystem = true
}

// applySystemOverrides narrows Database-level fields down to the single system
// this run will check, merging command-line flags into it field by field.
//
// Merging rather than rebuilding matters: replacing the struct wholesale dropped
// every field the caller did not pass, so `-dir ./other` alone silently cleared
// the configured schema and sent the EXPLAINs to whatever Database.Schema said.
// That failure mode is why the multi-system branch looks a system up by name
// instead of fabricating one — a fabricated system has no schema and no xml_dir,
// which reproduces the same silent wrong-database behaviour on the very path the
// error text recommends ("narrow with -sys").
func (c *Config) applySystemOverrides(sys, dir, schema string) error {
	switch {
	case len(c.Systems) <= 1:
		if len(c.Systems) == 0 {
			c.ensureSyntheticSystem()
		}
		sc := &c.Systems[0]
		if sys != "" {
			// A default-built placeholder is not something the user declared, so
			// naming it is a legitimate rename. Naming anything else differently
			// is a typo: the multi-system branch rejects unknown names, and a
			// silent relabel here would make the result table's sys column
			// disagree with the config history. This is decided by provenance,
			// not by comparing against "default" — a config may legitimately
			// declare a system with that name.
			if !c.syntheticSystem && sc.Name != sys {
				return fmt.Errorf("system %q is not declared in the config (declared: %s)",
					sys, sc.Name)
			}
			sc.Name = sys
		}
		if dir != "" {
			sc.XMLDir = dir
		}
		if schema != "" {
			sc.Schema = schema
		}

	case sys != "":
		// Several systems configured and one was named: narrow to it, keeping
		// everything the user did not override.
		for i := range c.Systems {
			if c.Systems[i].Name == sys {
				if dir != "" {
					c.Systems[i].XMLDir = dir
				}
				if schema != "" {
					c.Systems[i].Schema = schema
				}
				c.Systems = c.Systems[i : i+1]
				return nil
			}
		}
		return fmt.Errorf("system %q is not declared in the config (available: %s)",
			sys, strings.Join(systemNames(c.Systems), ", "))

	default:
		// Several systems and nothing said which one. A bare -dir/-schema
		// cannot identify the system it applies to, so refuse instead of
		// guessing and checking the wrong XML against the wrong schema.
		if dir != "" || schema != "" {
			return fmt.Errorf("%d systems configured; name one with -sys to apply -dir/-schema",
				len(c.Systems))
		}
	}
	return nil
}

func systemNames(systems []SystemConfig) []string {
	names := make([]string, 0, len(systems))
	for _, s := range systems {
		names = append(names, s.Name)
	}
	return names
}

// ResolveTargetSchema returns the schema EXPLAIN runs against.
//
// Precedence: the system's own schema, then the database-level default. The two
// can legitimately differ (a config may point the result table at one schema and
// the checked SQL at another), which is why this is explicit rather than an
// inline fallback.
func ResolveTargetSchema(systemSchema, dbSchema string) string {
	if systemSchema != "" {
		return systemSchema
	}
	return dbSchema
}
