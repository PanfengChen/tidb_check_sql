package sql

import (
	"testing"
)

func TestReplaceParams_Basic(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple hash param",
			input: "select * from t where id = #{id}",
			want:  "select * from t where id = 'x'",
		},
		{
			name:  "limit offset count",
			input: "select * from t limit #{offset}, #{count}",
			want:  "select * from t LIMIT 10, 10",
		},
		{
			name:  "limit single",
			input: "select * from t limit #{count}",
			want:  "select * from t LIMIT 100",
		},
		{
			name:  "quoted date param",
			input: "where dt = '#{date}'",
			want:  "where dt = '20230301'",
		},
		{
			name:  "jdbcType date",
			input: "where dt = #{date,jdbcType=DATE}",
			want:  "where dt = '20230301'",
		},
		{
			name:  "legacy quoted param",
			input: "where col = '#param#'",
			want:  "where col = '20230302'",
		},
		{
			name:  "legacy unquoted param",
			input: "where col = #param#",
			want:  "where col = '20230302'",
		},
		{
			name:  "fetch rows only",
			input: "fetch first ${count} rows only",
			want:  "fetch first 100 ROWS ONLY",
		},
		{
			name:  "mixed params",
			input: "select * from t where name = #{name} and dt = '#{date}' and col = #legacy# limit #{offset}, #{count}",
			want:  "select * from t where name = 'x' and dt = '20230301' and col = '20230302' LIMIT 10, 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReplaceParams(tt.input)
			if got != tt.want {
				t.Errorf("ReplaceParams(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestReplaceParams_OrderPreserving(t *testing.T) {
	// This test verifies that rules are applied in the correct order.
	// If #{param} was matched before limit patterns, the limit output would be wrong.
	input := "select * from t limit #{offset}, #{count}"
	got := ReplaceParams(input)
	want := "select * from t LIMIT 10, 10"
	if got != want {
		t.Errorf("rule ordering broken: got %q, want %q", got, want)
	}
}

func TestReplaceParams_PagingPositions(t *testing.T) {
	// R4: pagination positions (LIMIT / OFFSET / ROWNUM) must yield integers, never
	// the default legacy string '20230302' — an OFFSET 'string' is a hard syntax error.
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "iBATIS limit offset single",
			input: "select * from t limit #maxRows# offset #startRow#",
			want:  "select * from t LIMIT 100 OFFSET 10",
		},
		{
			name:  "iBATIS limit comma pair",
			input: "select * from t limit #start#, #count#",
			want:  "select * from t LIMIT 10, 10",
		},
		{
			name:  "MyBatis offset",
			input: "select * from t limit 100 offset #{startRow}",
			want:  "select * from t limit 100 OFFSET 10",
		},
		{
			name:  "iBATIS rownum between",
			input: "select * from t where rownum between #lo# and #hi#",
			want:  "select * from t where ROWNUM BETWEEN 1 AND 20",
		},
		{
			name:  "oracle rownum le",
			input: "select * from t where rownum <= #maxRows#",
			want:  "select * from t where ROWNUM <= 10",
		},
		{
			name:  "oracle rownum lt",
			input: "select * from t where rownum < #{max}",
			want:  "select * from t where ROWNUM < 10",
		},
		{
			name:  "oracle rownum no-op",
			input: "select * from t where rownum = #rn#",
			want:  "select * from t where ROWNUM = 10",
		},
		{
			name:  "db2 fetch first plural",
			input: "select * from t fetch first #cnt# rows only",
			want:  "select * from t FETCH FIRST 100 ROWS ONLY",
		},
		{
			name:  "db2 fetch first singular row",
			input: "select * from t fetch first 1 row only fetch first #cnt2# row only",
			want:  "select * from t fetch first 1 row only FETCH FIRST 100 ROWS ONLY",
		},
		{
			name:  "mixed legacy normal param + offset stays ordered",
			input: "select * from t where col = #val# and dt = '#dt#' limit #n# offset #off#",
			want:  "select * from t where col = '20230302' and dt = '20230302' LIMIT 100 OFFSET 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReplaceParams(tt.input)
			if got != tt.want {
				t.Errorf("ReplaceParams(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSQLTypeDummyVal(t *testing.T) {
	tests := []struct {
		jdbcType string
		want     string
	}{
		{"VARCHAR", "'x'"},
		{"INTEGER", "1"},
		{"BIGINT", "1"},
		{"DECIMAL", "1.0"},
		{"DATE", "'2023-01-01'"},
		{"TIME", "'12:00:00'"},
		{"TIMESTAMP", "'2023-01-01 12:00:00'"},
		{"BOOLEAN", "TRUE"},
		{"BLOB", "X'00'"},
		{"UNKNOWN", "'x'"},
	}

	for _, tt := range tests {
		t.Run(tt.jdbcType, func(t *testing.T) {
			got := SQLTypeDummyVal(tt.jdbcType)
			if got != tt.want {
				t.Errorf("SQLTypeDummyVal(%q) = %q, want %q", tt.jdbcType, got, tt.want)
			}
		})
	}
}
