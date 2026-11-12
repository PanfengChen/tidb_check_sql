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
		c.Systems = []SystemConfig{{Name: "default", XMLDir: "."}}
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
	if !strings.Contains(c.Database.Table, ".") {
		c.Database.Table = c.Database.Schema + "." + c.Database.Table
	}
}

// CLI overrides: command-line flags take precedence over config file values.
func (c *Config) ApplyCLIOverrides(host string, port int, user, password, schema, table, sys, dir, output string) {
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
	// Only an explicitly supplied -sys/-dir replaces the system list. main used
	// to pass the flag defaults ("default"/".") here unconditionally, which
	// overwrote every system read from the config file with a single empty one.
	// Callers must pass "" for a flag the user did not set.
	if sys != "" || dir != "" {
		sc := SystemConfig{Name: sys, XMLDir: dir, Schema: schema}
		if sc.Name == "" {
			sc.Name = "default"
		}
		if sc.XMLDir == "" {
			sc.XMLDir = "."
		}
		c.Systems = []SystemConfig{sc}
	}
	c.NormalizeTableName()
}
