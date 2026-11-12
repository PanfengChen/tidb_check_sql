package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestVerifyOutputFormats(t *testing.T) {
	dir := t.TempDir()

	s := Summary{
		System:  "sys1",
		Total:   2,
		Success: 1,
		Failed:  1,
		Details: []RowDetail{
			{
				ModName: "OrderMapper", SQLID: "findById", OldSQL: "select * from t",
				TestSQL: "select * from t", Matched: "F", ErrMsg: "table not found",
				Suggestion: `[{"category":"syntax","title":"bad","suggestion":"fix it","rewrite":"x->y","reference":"http://x"},{"category":"other","title":"t2","suggestion":"s2","rewrite":"","reference":""}]`,
			},
			{
				ModName: "UserMapper", SQLID: "findAll", OldSQL: "select * from u",
				TestSQL: "select * from u", Matched: "T", ErrMsg: "", Suggestion: "",
			},
		},
	}

	// JSON
	if err := FormatAndPrint(s, "json", dir); err != nil {
		t.Fatalf("json: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	var out struct {
		Details []struct {
			SQLID       string                   `json:"sqlid"`
			Suggestions []map[string]interface{} `json:"suggestions"`
		} `json:"details"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if len(out.Details) != 2 {
		t.Fatalf("json details = %d, want 2", len(out.Details))
	}
	if len(out.Details[0].Suggestions) != 2 {
		t.Fatalf("json suggestions[0] = %d, want 2", len(out.Details[0].Suggestions))
	}
	if out.Details[0].Suggestions[0]["category"] != "syntax" {
		t.Fatalf("json suggestion category = %v, want syntax", out.Details[0].Suggestions[0]["category"])
	}
	if out.Details[1].Suggestions == nil {
		t.Fatalf("json suggestions[1] should be empty slice, got nil")
	}

	// Excel
	if err := FormatAndPrint(s, "excel", dir); err != nil {
		t.Fatalf("excel: %v", err)
	}
	xlsx, err := excelize.OpenFile(filepath.Join(dir, "report.xlsx"))
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	defer xlsx.Close()
	rows, err := xlsx.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	// header + 2 (first SQL expanded to 2 rows) + 1 (second SQL) = 4
	if len(rows) != 4 {
		t.Fatalf("xlsx rows = %d, want 4", len(rows))
	}
	if len(rows[0]) != 11 {
		t.Fatalf("xlsx header cols = %d, want 11", len(rows[0]))
	}
	if rows[0][7] != "title" || rows[0][10] != "reference" {
		t.Fatalf("xlsx header mismatch: %v", rows[0])
	}
	if rows[1][1] != "findById" || rows[1][6] != "syntax" || rows[1][9] != "x->y" {
		t.Fatalf("xlsx row1 mismatch: %v", rows[1])
	}
	if rows[2][1] != "findById" || rows[2][6] != "other" || rows[2][8] != "s2" {
		t.Fatalf("xlsx row2 mismatch: %v", rows[2])
	}
	if rows[3][1] != "findAll" || len(rows[3]) > 6 {
		t.Fatalf("xlsx row3 mismatch: %v", rows[3])
	}

	// HTML
	if err := FormatAndPrint(s, "html", dir); err != nil {
		t.Fatalf("html: %v", err)
	}
	htmlRaw, _ := os.ReadFile(filepath.Join(dir, "index.html"))
	htmlStr := string(htmlRaw)
	for _, want := range []string{"copy-btn", "copySQLID", "user-select:text", "📋"} {
		if !strings.Contains(htmlStr, want) {
			t.Fatalf("html missing %q", want)
		}
	}
	if strings.Contains(htmlStr, `title="点击复制 SQLID"`) {
		t.Fatalf("html should not contain misleading tooltip")
	}
}

func TestUnknownFormatReturnsError(t *testing.T) {
	s := Summary{}
	if err := FormatAndPrint(s, "excell", t.TempDir()); err == nil {
		t.Fatalf("expected error for unknown format, got nil")
	}
}
