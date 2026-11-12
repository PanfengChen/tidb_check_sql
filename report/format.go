package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"check_sql_go/advisor"
)

// Summary holds aggregate statistics for a check run.
type Summary struct {
	System    string
	Total     int
	Success   int
	Failed    int
	Details   []RowDetail
	StartTime time.Time
}

// RowDetail holds per-SQL check results.
type RowDetail struct {
	ModName    string
	SQLID      string
	OldSQL     string
	TestSQL    string
	Matched    string
	ErrMsg     string
	Suggestion string
}

// parseSuggestions decodes the JSON-encoded suggestion column into a slice of
// advisor hints. It returns an empty (non-nil) slice when there is nothing to
// decode. A malformed payload (e.g. a legacy plain-text suggestion) is treated
// as empty rather than failing the whole report.
func parseSuggestions(raw string) []advisor.SuggestionHint {
	hints := make([]advisor.SuggestionHint, 0)
	if raw == "" {
		return hints
	}
	_ = json.Unmarshal([]byte(raw), &hints)
	return hints
}

// FormatAndPrint outputs results in the requested format.
// For html/json/csv/excel formats, outputDir is the directory where the file is written.
// For terminal format, output goes to stdout.
func FormatAndPrint(s Summary, format, outputDir string) error {
	switch strings.ToLower(format) {
	case "html":
		return outputHTML(s, outputDir)
	case "json":
		return outputJSONFile(s, outputDir)
	case "csv":
		return outputCSVFile(s, outputDir)
	case "excel", "xlsx":
		return outputExcel(s, outputDir)
	case "terminal":
		outputTerminal(s)
		return nil
	default:
		return fmt.Errorf("unknown output format %q: supported formats are html, json, csv, excel, terminal", format)
	}
}

// DefaultOutputDir returns a timestamped directory name.
func DefaultOutputDir() string {
	return "check_report_" + time.Now().Format("20060102_150405")
}

// outputHTML generates a self-contained HTML report.
func outputHTML(s Summary, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create output directory %s: %w", dir, err)
	}

	path := filepath.Join(dir, "index.html")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("cannot create %s: %w", path, err)
	}
	defer f.Close()

	passRate := 0.0
	if s.Total > 0 {
		passRate = float64(s.Success) / float64(s.Total) * 100
	}

	f.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>TiDB SQL 兼容性检查报告</title>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Noto Sans SC",sans-serif;background:#f5f7fa;color:#1a1a2e;line-height:1.6}
.header{background:linear-gradient(135deg,#1a1a2e,#16213e);color:#fff;padding:40px 32px;text-align:center}
.header h1{font-size:28px;font-weight:700;margin-bottom:8px}
.header .sub{font-size:14px;opacity:.75}
.container{max-width:1200px;margin:0 auto;padding:24px}
.cards{display:grid;grid-template-columns:repeat(4,1fr);gap:16px;margin-bottom:32px}
.card{background:#fff;border-radius:12px;padding:24px;box-shadow:0 1px 3px rgba(0,0,0,.08);text-align:center}
.card .value{font-size:36px;font-weight:800;line-height:1.2}
.card .label{font-size:13px;color:#888;margin-top:4px;text-transform:uppercase;letter-spacing:.5px}
.card.total .value{color:#1a1a2e}
.card.pass .value{color:#0d9488}
.card.fail .value{color:#ef4444}
.card.rate .value{color:#6366f1}
.gauge{background:#e5e7eb;border-radius:999px;height:12px;margin:16px 0 24px;overflow:hidden}
.gauge-fill{height:100%;border-radius:999px;transition:width .6s ease}
.gauge-fill.green{background:linear-gradient(90deg,#0d9488,#10b981)}
.gauge-fill.yellow{background:linear-gradient(90deg,#f59e0b,#fbbf24)}
.gauge-fill.red{background:linear-gradient(90deg,#ef4444,#f87171)}
.section{margin-bottom:24px}
.section-title{font-size:18px;font-weight:700;margin-bottom:16px;display:flex;align-items:center;gap:8px}
.section-title .icon{font-size:20px}
.tabs{display:flex;gap:8px;margin-bottom:16px}
.tab{padding:8px 20px;border-radius:999px;border:1px solid #d1d5db;background:#fff;cursor:pointer;font-size:13px;font-weight:600;transition:all .15s}
.tab:hover{background:#f3f4f6}
.tab.active{background:#1a1a2e;color:#fff;border-color:#1a1a2e}
.row{border-radius:12px;background:#fff;box-shadow:0 1px 3px rgba(0,0,0,.06);margin-bottom:10px;overflow:hidden}
.row-header{display:flex;align-items:center;padding:16px 20px;cursor:pointer;gap:12px;user-select:none}
.row-header:hover{background:#fafbfc}
.row.pass .row-header{border-left:4px solid #10b981}
.row.fail .row-header{border-left:4px solid #ef4444}
.row-badge{display:inline-flex;align-items:center;justify-content:center;width:28px;height:28px;border-radius:50%;font-size:14px;flex-shrink:0}
.row.pass .row-badge{background:#d1fae5;color:#065f46}
.row.fail .row-badge{background:#fee2e2;color:#991b1b}
.row-id{font-weight:700;font-family:"JetBrains Mono","Fira Code",monospace;font-size:14px;flex-shrink:0;min-width:120px;user-select:text;cursor:text}
.copy-btn{display:inline-flex;align-items:center;justify-content:center;width:24px;height:24px;border:none;background:#eef2f7;border-radius:6px;cursor:pointer;font-size:12px;line-height:1;flex-shrink:0;transition:background .15s}
.copy-btn:hover{background:#dbe4f0}
.row-mod{color:#666;font-size:13px;flex:1}
.row-sql{font-family:"JetBrains Mono","Fira Code",monospace;font-size:11px;color:#555;flex:2;overflow:hidden;white-space:nowrap;text-overflow:ellipsis;min-width:0}
.hint-badge{display:inline-flex;align-items:center;gap:4px;background:#eff6ff;color:#2563eb;font-size:11px;font-weight:700;padding:3px 10px;border-radius:999px;flex-shrink:0}
.row-detail{display:none;padding:0 20px 20px 20px;border-top:1px solid #f3f4f6}
.row.open .row-detail{display:block}
.sql-box{background:#1e1e2e;color:#cdd6f4;border-radius:8px;padding:16px;font-family:"JetBrains Mono","Fira Code",monospace;font-size:12px;line-height:1.8;overflow-x:auto;white-space:pre-wrap;word-break:break-word;margin:8px 0;max-height:none}
.sql-box .kw{color:#cba6f7}.sql-box .fn{color:#89b4fa}.sql-box .str{color:#a6e3a1}.sql-box .num{color:#fab387}.sql-box .cmt{color:#6c7086;font-style:italic}
.error-box{background:#fef2f2;border:1px solid #fecaca;border-radius:8px;padding:12px 16px;color:#991b1b;font-family:"JetBrains Mono","Fira Code",monospace;font-size:12px;margin:8px 0;white-space:pre-wrap;word-break:break-word}
.hint-list{list-style:none;padding:0;margin:8px 0}
.hint-item{background:#eff6ff;border:1px solid #bfdbfe;border-radius:8px;padding:14px 16px;margin:8px 0}
.hint-item .hint-cat{display:inline-block;background:#dbeafe;color:#1e40af;font-size:10px;font-weight:700;padding:2px 8px;border-radius:999px;text-transform:uppercase;margin-bottom:6px}
.hint-item .hint-title{font-weight:700;color:#1e3a5f;margin-bottom:4px;font-size:14px}
.hint-item .hint-sugg{font-size:14px;color:#333;line-height:1.6}
.hint-item .hint-rewrite{font-family:"JetBrains Mono","Fira Code",monospace;font-size:12px;background:#dbeafe;padding:6px 12px;border-radius:6px;display:inline-block;margin-top:8px;color:#1e40af;white-space:pre-wrap;word-break:break-word;max-width:100%}
.sql-label{font-size:11px;color:#888;margin-bottom:4px;font-weight:600;display:flex;align-items:center;gap:6px}
.sql-label::before{content:'';display:inline-block;width:8px;height:8px;border-radius:50%;background:#94a3b8}
.section-sep{height:1px;background:#e5e7eb;margin:12px 0}
.empty-state{text-align:center;padding:60px 20px;color:#9ca3af}
.empty-state .icon{font-size:48px;margin-bottom:12px}
.footer{text-align:center;padding:32px;color:#9ca3af;font-size:12px;border-top:1px solid #e5e7eb;margin-top:32px}
</style>
<script>
function toggleRow(el){el.closest(".row").classList.toggle("open")}
function copySQLID(event,btn){event.stopPropagation();var id=btn.closest(".row-header").querySelector(".row-id").textContent;if(navigator.clipboard&&navigator.clipboard.writeText){navigator.clipboard.writeText(id).then(function(){btn.textContent="✅";setTimeout(function(){btn.textContent="📋"},1200)},function(){fallbackCopy(btn,id)})}else{fallbackCopy(btn,id)}}
function fallbackCopy(btn,text){var ta=document.createElement("textarea");ta.value=text;document.body.appendChild(ta);ta.select();try{document.execCommand("copy")}catch(e){}document.body.removeChild(ta);btn.textContent="✅";setTimeout(function(){btn.textContent="📋"},1200)}
function filterRows(tab){
  document.querySelectorAll(".tab").forEach(t=>t.classList.remove("active"));
  tab.classList.add("active");
  var filter=tab.dataset.filter;
  document.querySelectorAll(".row").forEach(r=>{
    if(filter==="all"){r.style.display="";return}
    if(filter==="fail"){r.style.display=r.classList.contains("fail")?"":"none"}
    if(filter==="pass"){r.style.display=r.classList.contains("pass")?"":"none"}
  });
}
</script>
</head>
<body>
<div class="header">
  <h1>🔍 TiDB SQL 兼容性检查报告</h1>
  <div class="sub">` + html.EscapeString(s.System) + ` &nbsp;·&nbsp; ` + time.Now().Format("2006-01-02 15:04:05") + `</div>
</div>
<div class="container">
`)

	// Summary cards
	gaugeColor := "green"
	if passRate < 60 {
		gaugeColor = "red"
	} else if passRate < 90 {
		gaugeColor = "yellow"
	}

	fmt.Fprintf(f, `<div class="cards">
  <div class="card total"><div class="value">%d</div><div class="label">SQL 总数</div></div>
  <div class="card pass"><div class="value">%d</div><div class="label">✓ 通过</div></div>
  <div class="card fail"><div class="value">%d</div><div class="label">✗ 失败</div></div>
  <div class="card rate"><div class="value">%.1f%%</div><div class="label">通过率</div></div>
</div>
<div class="gauge"><div class="gauge-fill %s" style="width:%.1f%%"></div></div>
`, s.Total, s.Success, s.Failed, passRate, gaugeColor, passRate)

	// Tabs
	f.WriteString(`<div class="tabs">
  <span class="tab active" data-filter="all" onclick="filterRows(this)">全部</span>
  <span class="tab" data-filter="fail" onclick="filterRows(this)">❌ 失败</span>
  <span class="tab" data-filter="pass" onclick="filterRows(this)">✅ 通过</span>
</div>
`)

	// Results
	if len(s.Details) == 0 {
		f.WriteString(`<div class="empty-state"><div class="icon">📭</div><p>没有可显示的 SQL 语句</p></div>`)
	} else {
		for _, d := range s.Details {
			rowClass := "pass"
			badgeIcon := "✓"
			if d.Matched != "T" {
				rowClass = "fail"
				badgeIcon = "✗"
			}

			// Parse suggestion count for badge
			suggCount := 0
			if d.Suggestion != "" {
				var hints []advisor.SuggestionHint
				if json.Unmarshal([]byte(d.Suggestion), &hints) == nil {
					suggCount = len(hints)
				}
			}

			// Show full SQL in row header (CSS overflow hidden but no truncation)
			sqlPreview := d.TestSQL
			if sqlPreview == "" {
				sqlPreview = d.OldSQL
			}
			sqlPreview = strings.ReplaceAll(sqlPreview, "\n", " ")
			sqlPreview = strings.TrimSpace(sqlPreview)

			// Failed rows are always open
			openClass := ""
			if d.Matched != "T" {
				openClass = " open"
			}

			// Build hint badge
			hintBadge := ""
			if suggCount > 0 {
				hintBadge = fmt.Sprintf(`<span class="hint-badge">💡%d</span>`, suggCount)
			}

			fmt.Fprintf(f, `<div class="row %s%s">
  <div class="row-header" onclick="toggleRow(event)">
    <span class="row-badge">%s</span>
    <span class="row-id">%s</span>
    <button class="copy-btn" type="button" onclick="copySQLID(event, this)" title="复制 SQLID">📋</button>
    <span class="row-mod">%s</span>
    <span class="row-sql">%s</span>
    %s
  </div>
  <div class="row-detail">
`, rowClass, openClass, badgeIcon, html.EscapeString(d.SQLID), html.EscapeString(d.ModName), html.EscapeString(sqlPreview), hintBadge)

			// Original SQL
			if d.OldSQL != "" {
				f.WriteString(`    <div class="sql-label">📄 原始 SQL（从 XML 提取）</div>`)
				f.WriteString(`    <div class="sql-box">` + highlightSQL(html.EscapeString(d.OldSQL)) + `</div>`)
			}

			// Test SQL
			if d.TestSQL != "" && d.TestSQL != d.OldSQL {
				f.WriteString(`    <div class="sql-label">🧪 测试 SQL（参数替换后）</div>`)
				f.WriteString(`    <div class="sql-box">` + highlightSQL(html.EscapeString(d.TestSQL)) + `</div>`)
			}

			// Error
			if d.ErrMsg != "" {
				f.WriteString(`    <div class="error-box">❌ ` + html.EscapeString(d.ErrMsg) + `</div>`)
			}

			// Suggestions
			if d.Suggestion != "" {
				var hints []advisor.SuggestionHint
				if json.Unmarshal([]byte(d.Suggestion), &hints) == nil && len(hints) > 0 {
					fmt.Fprintf(f, `    <div class="sql-label" style="margin-top:12px">💡 改写建议（%d 条）</div><ul class="hint-list">`, len(hints))
					for _, h := range hints {
						f.WriteString(`<li class="hint-item">`)
						if h.Category != "" {
							fmt.Fprintf(f, `<span class="hint-cat">%s</span>`, html.EscapeString(h.Category))
						}
						fmt.Fprintf(f, `<div class="hint-title">%s</div>`, html.EscapeString(h.Title))
						fmt.Fprintf(f, `<div class="hint-sugg">%s</div>`, html.EscapeString(h.Suggestion))
						if h.Rewrite != "" {
							fmt.Fprintf(f, `<div class="hint-rewrite">✏️ %s</div>`, html.EscapeString(h.Rewrite))
						}
						f.WriteString(`</li>`)
					}
					f.WriteString(`</ul>`)
				}
			}

			f.WriteString(`  </div>
</div>
`)
		}
	}

	fmt.Fprintf(f, `</div>
<div class="footer">TiDB SQL Compatibility Checker &nbsp;·&nbsp; 生成于 %s &nbsp;·&nbsp; 总计 %d 条 SQL</div>
</body>
</html>`, time.Now().Format("2006-01-02 15:04:05"), s.Total)

	fmt.Printf("\n📄 HTML 报告已生成: %s\n", path)
	return nil
}

// highlightSQL adds syntax-highlighting span tags to SQL text.
// Input is already HTML-escaped.
func highlightSQL(sql string) string {
	// Keywords
	keywords := []string{
		"SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "IN", "LIKE", "BETWEEN",
		"INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE", "CREATE", "ALTER",
		"DROP", "TABLE", "INDEX", "JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "ON",
		"GROUP", "BY", "ORDER", "HAVING", "LIMIT", "OFFSET", "UNION", "ALL",
		"AS", "DISTINCT", "CASE", "WHEN", "THEN", "ELSE", "END",
		"EXISTS", "IS", "NULL", "ASC", "DESC", "EXPLAIN", "FOR", "SHARE",
	}
	for _, kw := range keywords {
		sql = strings.ReplaceAll(sql, kw, `<span class="kw">`+kw+`</span>`)
	}
	return sql
}

func outputTerminal(s Summary) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("  System: %s\n", s.System)
	fmt.Printf("  Total SQLs: %d | Success: %d | Failed: %d\n", s.Total, s.Success, s.Failed)
	fmt.Println(strings.Repeat("=", 70))

	for _, d := range s.Details {
		if d.Matched == "T" {
			if s.Failed == 0 {
				fmt.Printf("  ✅ %-20s %-30s\n", d.ModName, d.SQLID)
			}
		} else {
			fmt.Println(strings.Repeat("-", 70))
			fmt.Printf("  ❌ File: %s  ID: %s\n", d.ModName, d.SQLID)
			fmt.Printf("     SQL: %s\n", truncateStr(d.TestSQL, 120))
			fmt.Printf("     Error: %s\n", d.ErrMsg)
			if d.Suggestion != "" {
				var hints []advisor.SuggestionHint
				if json.Unmarshal([]byte(d.Suggestion), &hints) == nil {
					for i, h := range hints {
						fmt.Printf("     💡 [%d] %s: %s\n", i+1, h.Title, h.Suggestion)
						if h.Rewrite != "" {
							fmt.Printf("        改写: %s\n", h.Rewrite)
						}
					}
				}
			}
		}
	}
	fmt.Println()
}

func outputJSONFile(s Summary, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	type jsonDetail struct {
		ModName     string           `json:"modname"`
		SQLID       string           `json:"sqlid"`
		OldSQL      string           `json:"oldsql"`
		TestSQL     string           `json:"testsql"`
		Matched     string           `json:"matched"`
		ErrMsg      string           `json:"errmsg"`
		Suggestions []advisor.SuggestionHint `json:"suggestions"`
	}
	type jsonOutput struct {
		System  string       `json:"system"`
		Total   int          `json:"total"`
		Success int          `json:"success"`
		Failed  int          `json:"failed"`
		Details []jsonDetail `json:"details"`
	}
	details := make([]jsonDetail, 0, len(s.Details))
	for _, d := range s.Details {
		details = append(details, jsonDetail{
			ModName:     d.ModName,
			SQLID:       d.SQLID,
			OldSQL:      d.OldSQL,
			TestSQL:     d.TestSQL,
			Matched:     d.Matched,
			ErrMsg:      d.ErrMsg,
			Suggestions: parseSuggestions(d.Suggestion),
		})
	}
	path := filepath.Join(dir, "report.json")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(jsonOutput{
		System: s.System, Total: s.Total, Success: s.Success, Failed: s.Failed, Details: details,
	}); err != nil {
		return err
	}
	fmt.Printf("\n📄 JSON 报告已生成: %s\n", path)
	return nil
}

func outputCSVFile(s Summary, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, "report.csv")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"modname", "sqlid", "oldsql", "testsql", "matched", "errmsg", "suggestion"})
	for _, d := range s.Details {
		w.Write([]string{d.ModName, d.SQLID, d.OldSQL, d.TestSQL, d.Matched, d.ErrMsg, d.Suggestion})
	}
	w.Flush()
	fmt.Printf("\n📄 CSV 报告已生成: %s\n", path)
	return w.Error()
}

// outputExcel writes a .xlsx report with the suggestion column exploded into
// separate category/title/suggestion/rewrite/reference columns. A SQL with
// multiple suggestions is expanded into multiple rows so each hint is filterable.
func outputExcel(s Summary, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f := excelize.NewFile()
	defer f.Close()

	sheet := "Sheet1"
	headers := []string{
		"modname", "sqlid", "oldsql", "testsql", "matched", "errmsg",
		"category", "title", "suggestion", "rewrite", "reference",
	}

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#1A1A2E"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err != nil {
		return err
	}

	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return err
		}
	}
	lastCol, _ := excelize.CoordinatesToCellName(len(headers), 1)
	if err := f.SetCellStyle(sheet, "A1", lastCol+"1", headerStyle); err != nil {
		return err
	}

	row := 2
	for _, d := range s.Details {
		hints := parseSuggestions(d.Suggestion)
		if len(hints) == 0 {
			// Emit a single base row so SQLs without suggestions still appear.
			hints = []advisor.SuggestionHint{{}}
		}
		for _, h := range hints {
			values := []string{
				d.ModName, d.SQLID, d.OldSQL, d.TestSQL, d.Matched, d.ErrMsg,
				h.Category, h.Title, h.Suggestion, h.Rewrite, h.Reference,
			}
			for j, v := range values {
				cell, _ := excelize.CoordinatesToCellName(j+1, row)
				if err := f.SetCellValue(sheet, cell, v); err != nil {
					return err
				}
			}
			row++
		}
	}

	widths := map[string]float64{
		"A": 20, "B": 22, "C": 50, "D": 50, "E": 10, "F": 45,
		"G": 22, "H": 30, "I": 50, "J": 40, "K": 40,
	}
	for col, w := range widths {
		if err := f.SetColWidth(sheet, col, col, w); err != nil {
			return err
		}
	}

	if err := f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	}); err != nil {
		return err
	}

	if row > 2 {
		wrapStyle, err := f.NewStyle(&excelize.Style{
			Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		})
		if err != nil {
			return err
		}
		lastRowCell := fmt.Sprintf("%s%d", lastCol, row-1)
		if err := f.SetCellStyle(sheet, "A2", lastRowCell, wrapStyle); err != nil {
			return err
		}
	}

	path := filepath.Join(dir, "report.xlsx")
	if err := f.SaveAs(path); err != nil {
		return fmt.Errorf("cannot save %s: %w", path, err)
	}
	fmt.Printf("\n📊 Excel 报告已生成: %s\n", path)
	return nil
}

// truncateStr truncates to at most maxLen bytes plus an ellipsis, backing off
// to a rune boundary so a multi-byte character is never split in half, and
// collapsing newlines first so the result stays on one console line.
func truncateStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if maxLen <= 0 {
		return ""
	}
	s = strings.ToValidUTF8(s, "")
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
