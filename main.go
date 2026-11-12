package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"check_sql_go/advisor"
	"check_sql_go/checker"
	"check_sql_go/config"
	"check_sql_go/parser"
	"check_sql_go/report"
	sqlpkg "check_sql_go/sql"
)

// Version information. These are package-level variables so they can be
// overridden at build time via -ldflags, e.g.:
//
//	go build -ldflags "-X main.version=v1.0.0 -X main.buildDate=2026-08-14" .
var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func main() {
	os.Exit(run())
}

// reorderArgs moves positional (non-flag) arguments to the end of os.Args
// so that Go's flag.Parse() can process all flags correctly regardless of
// where they appear on the command line.
// Returns the reordered args and true if reordering was needed.
func reorderArgs() ([]string, bool) {
	raw := os.Args[1:]
	var flags []string
	var positional []string
	reordered := false
	seenPositional := false

	for i := 0; i < len(raw); i++ {
		arg := raw[i]
		if strings.HasPrefix(arg, "-") {
			if seenPositional {
				reordered = true
			}
			flags = append(flags, arg)
			// Consume the next arg as the flag's value if it doesn't look like a flag
			if i+1 < len(raw) && !strings.HasPrefix(raw[i+1], "-") {
				i++
				flags = append(flags, raw[i])
			}
		} else {
			seenPositional = true
			positional = append(positional, arg)
		}
	}

	result := append([]string{os.Args[0]}, append(flags, positional...)...)
	return result, reordered
}

func run() int {
	// CLI flags (compatible with both flag-based and positional arg usage)
	var (
		xmlDir      string
		host        string
		port        int
		user        string
		password    string
		schema      string
		table       string
		sysName     string
		output      string
		configFile  string
		showVersion bool
	)

	flag.StringVar(&xmlDir, "dir", "", "Directory containing MyBatis XML files")
	flag.StringVar(&host, "host", "", "TiDB server host")
	flag.IntVar(&port, "port", 0, "TiDB server port")
	flag.StringVar(&user, "user", "", "Database user")
	flag.StringVar(&password, "password", "", "Database password")
	flag.StringVar(&schema, "schema", "", "Target database schema")
	flag.StringVar(&table, "table", "", "Result table name (e.g., test.all_sqls)")
	flag.StringVar(&sysName, "sys", "", "System name for this batch")
	flag.StringVar(&output, "output", "html", "Output format: html, json, csv, excel, terminal")
	flag.StringVar(&configFile, "config", "", "TOML config file path")
	flag.BoolVar(&showVersion, "version", false, "Print version information and exit")

	// Reorder args so positional parameters don't cause flag.Parse() to stop early.
	// Go's flag package stops at the first non-flag argument by default.
	reorderedArgs, wasReordered := reorderArgs()
	os.Args = reorderedArgs
	flag.Parse()

	// Remember which flags the user actually supplied. Defaults must not be
	// treated as intent: passing them through to ApplyCLIOverrides overwrote
	// every system declared in the config file.
	explicit := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

	// Handle -version before any other work (no config/DB needed).
	if showVersion {
		fmt.Printf("check_sql %s\n", version)
		fmt.Printf("  commit: %s\n", gitCommit)
		fmt.Printf("  built:  %s\n", buildDate)
		return 0
	}

	if wasReordered {
		fmt.Fprintf(os.Stderr, "WARNING: positional arguments should be placed after all flags.\n")
		fmt.Fprintf(os.Stderr, "          The command has been auto-corrected, but please use the form:\n")
		fmt.Fprintf(os.Stderr, "          check_sql [flags] <xml-directory>\n")
	}

	// Support positional argument as xmlDir shortcut (backward compat)
	xmlDirFromCLI := xmlDir != ""
	if xmlDir == "" && flag.NArg() > 0 {
		xmlDir = flag.Arg(0)
		xmlDirFromCLI = true
	}

	// Load configuration
	cfg := &config.Config{}
	if configFile != "" {
		var err error
		cfg, err = config.LoadTOML(configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			return 1
		}
	}
	cfg.ApplyDefaults()

	// Hand over only what the user actually typed; "" means "not specified".
	cliSys, cliDir := "", ""
	if explicit["sys"] {
		cliSys = sysName
	}
	if xmlDirFromCLI {
		cliDir = xmlDir
	}
	cfg.ApplyCLIOverrides(host, port, user, password, schema, table, cliSys, cliDir, output)

	// Checked after the overrides: passing -sys/-dir narrows the run to one
	// system, so it must not be rejected by a guard that ran earlier. Only a
	// config that still declares several systems is refused. Silently using the
	// first one produced a report that looked complete while covering a
	// fraction of the XML, and — with the result table keyed by
	// (sys, modname, sqlid) — let same-named SQL from different systems
	// overwrite each other.
	if len(cfg.Systems) > 1 {
		fmt.Fprintf(os.Stderr, "ERROR: %d systems configured, but only one system is checked per run.\n", len(cfg.Systems))
		fmt.Fprintf(os.Stderr, "       Split the config and run once per system, or narrow with -sys/-dir.\n")
		return 1
	}

	// The config's xml_dir applies only when no directory was given on the
	// command line; a CLI path always wins.
	if !xmlDirFromCLI && len(cfg.Systems) > 0 && cfg.Systems[0].XMLDir != "" {
		xmlDir = cfg.Systems[0].XMLDir
	}
	if xmlDir == "" {
		xmlDir = "."
	}

	// Resolve absolute XML directory path
	absDir, err := filepath.Abs(xmlDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot resolve path %s: %v\n", xmlDir, err)
		return 1
	}

	fmt.Printf("check_sql %s — TiDB SQL Compatibility Checker\n", version)
	fmt.Printf("XML directory: %s\n", absDir)
	fmt.Printf("Database: %s@%s:%d\n", cfg.Database.User, cfg.Database.Host, cfg.Database.Port)

	// Parse XML files
	fmt.Printf("\n[1/4] Parsing MyBatis XML files...\n")
	parseResult, err := parser.ParseDirectory(absDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}
	fmt.Printf("       Parsed %d SQL statements from XML files\n", len(parseResult.Statements))
	for _, e := range parseResult.SQLErrors {
		fmt.Printf("       WARN: %s\n", e)
	}

	if len(parseResult.Statements) == 0 {
		fmt.Println("No SQL statements found. Exiting.")
		return 0
	}

	// Connect to TiDB
	fmt.Printf("\n[2/4] Connecting to TiDB...\n")
	chk, err := checker.NewChecker(checker.DBConfig{
		Host:   cfg.Database.Host,
		Port:   cfg.Database.Port,
		User:   cfg.Database.User,
		Passwd: cfg.Database.Password,
		Schema: cfg.Database.Schema,
		Table:  cfg.Database.Table,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}
	defer chk.Close()

	// Initialize result table
	if err := chk.InitTable(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}
	fmt.Printf("       Connected. Result table: %s\n", cfg.Database.Table)

	// Phase 1: Store extracted SQLs (initSqls equivalent)
	fmt.Printf("\n[3/4] Storing extracted SQLs...\n")

	if len(cfg.Systems) == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: no system configured\n")
		return 1
	}
	sys := cfg.Systems[0].Name
	targetSchema := config.ResolveTargetSchema(cfg.Systems[0].Schema, cfg.Database.Schema)

	insertOK, insertFail := 0, 0
	for _, stmt := range parseResult.Statements {
		stmt.Sys = sys
		err := chk.InsertOrUpdateSQL(sys, stmt.ModName, stmt.SQLID, stmt.OldSQL)
		if err != nil {
			fmt.Printf("       FAIL: %s -> %s: %v\n", stmt.ModName, stmt.SQLID, err)
			insertFail++
		} else {
			fmt.Printf("       OK: %-30s -> %s\n", stmt.ModName, stmt.SQLID)
			insertOK++
		}
	}
	fmt.Printf("       Inserted: %d ok, %d failed\n", insertOK, insertFail)

	// Phase 2: Check SQLs with EXPLAIN
	fmt.Printf("\n[4/4] Running EXPLAIN checks on schema '%s'...\n", targetSchema)
	adv := advisor.NewAdvisor()

	var summary report.Summary
	summary.System = sys

	total, success, failed, writeErrors := 0, 0, 0, 0
	for _, stmt := range parseResult.Statements {
		// Apply parameter substitution to generate test SQL
		testSQL := sqlpkg.ExtractAndReplace(stmt.OldSQL)
		stmt.TestSQL = testSQL

		// Execute EXPLAIN
		matched, errMsg := chk.ExplainSQL(targetSchema, testSQL)
		stmt.Matched = matched
		stmt.ErrMsg = errMsg

		// Generate suggestion for failed SQLs
		var suggestion string
		if matched == "F" {
			result := adv.Analyze(stmt.SQLID, testSQL, errMsg)
			suggestion = result.ToJSON()
			failed++
		} else {
			success++
		}
		total++

		// Update result table
		updErr := chk.UpdateResult(sys, stmt.ModName, stmt.SQLID, testSQL, matched, errMsg, suggestion)
		if updErr != nil {
			// A failed UPDATE means this row keeps the values from the last run
			// (REPLACE resets them every run), so the report and the table
			// disagree. Surface it: count it and fail the run.
			fmt.Printf("       UPDATE FAIL: %s:%s — %v\n", stmt.ModName, stmt.SQLID, updErr)
			writeErrors++
		}

		status := "✅"
		if matched == "F" {
			status = "❌"
		}
		fmt.Printf("       %s %-30s %-40s %s\n", status, stmt.ModName, stmt.SQLID, stmt.ErrMsg)

		summary.Details = append(summary.Details, report.RowDetail{
			ModName:    stmt.ModName,
			SQLID:      stmt.SQLID,
			OldSQL:     stmt.OldSQL,
			TestSQL:    testSQL,
			Matched:    matched,
			ErrMsg:     errMsg,
			Suggestion: suggestion,
		})
	}

	summary.Total = total
	summary.Success = success
	summary.Failed = failed

	// Determine output directory and format
	outputDir := report.DefaultOutputDir()
	format := cfg.Output.Format
	if format == "terminal" {
		outputDir = "" // terminal uses stdout directly
	}

	// Print final report
	if err := report.FormatAndPrint(summary, format, outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}

	if writeErrors > 0 {
		fmt.Fprintf(os.Stderr, "ERROR: %d of %d result rows could not be written back\n", writeErrors, total)
		return 1
	}
	if failed > 0 {
		return 1
	}
	return 0
}
