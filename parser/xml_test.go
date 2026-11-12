package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sqlpkg "check_sql_go/sql"
)

// TestParseDemoXMLs tests parsing against the sample mapper XMLs shipped in the
// repository (xmls/). The path is repository-relative so a fresh clone runs it
// too — an earlier version pointed at ../../check_original/check/xmls, a
// directory that only existed on the author's machine, so every other clone
// silently skipped this test.
func TestParseDemoXMLs(t *testing.T) {
	demoDir := filepath.Join("..", "xmls")
	if _, err := os.Stat(demoDir); err != nil {
		t.Fatalf("sample XML directory %s is missing: %v", demoDir, err)
	}

	result, err := ParseDirectory(demoDir)
	if err != nil {
		t.Fatalf("ParseDirectory failed: %v", err)
	}

	if len(result.Statements) == 0 {
		t.Fatal("expected at least 1 SQL statement, got 0")
	}

	t.Logf("Parsed %d SQL statements from demo XMLs", len(result.Statements))
	for _, s := range result.Statements {
		t.Logf("  %s[%s]: %s", s.SQLType, s.SQLID, s.OldSQL)
	}

	// AC-5: a.xml should have at least 1 statement (the original has 1 select)
	aXMLStatements := 0
	for _, s := range result.Statements {
		if filepath.Base(s.File) == "a.xml" {
			aXMLStatements++
		}
	}
	if aXMLStatements == 0 {
		t.Error("a.xml: expected at least 1 SQL statement, got 0")
	}
}

// TestParseXMLWithAndTag tests parsing of the custom <and> tag.
func TestParseXMLWithAndTag(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<select id="testSelect">
    <sql type="Str">
        select * from test.t
    </sql>
    <where>
        <and type="And">
            <if test="a!=null"><str type="Str"><![CDATA[cola=#cola#]]></str></if>
            <if test="b!=null"><str type="Str"><![CDATA[colb=#colb#]]></str></if>
        </and>
    </where>
</select>`

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.xml")
	if err := os.WriteFile(tmpFile, []byte(xmlContent), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ParseDirectory(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(result.Statements))
	}

	sql := result.Statements[0].OldSQL
	t.Logf("Generated SQL: %s", sql)

	// Should contain the where clause and the and connector
	if sql == "" {
		t.Error("SQL should not be empty")
	}
}

// TestDynamicTags tests parsing of common dynamic tags.
func TestDynamicTags(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<select id="dynamicTest">
    select * from test.t
    <where>
        <if test="name != null">and name = #{name}</if>
        <choose>
            <when test="status != null">and status = #{status}</when>
            <otherwise>and 1=1</otherwise>
        </choose>
        <foreach collection="ids" item="id" open=" and id in (" close=")" separator=",">
            #{id}
        </foreach>
    </where>
</select>`

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.xml")
	if err := os.WriteFile(tmpFile, []byte(xmlContent), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ParseDirectory(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(result.Statements))
	}

	t.Logf("Generated SQL: %s", result.Statements[0].OldSQL)
}

// TestIncludeResolution tests same-file <include> resolution.
func TestIncludeResolution(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<sql id="baseColumns">id, name, status</sql>
<select id="useInclude">
    select <include refid="baseColumns"/> from test.t
</select>`

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.xml")
	if err := os.WriteFile(tmpFile, []byte(xmlContent), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ParseDirectory(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(result.Statements))
	}

	sql := result.Statements[0].OldSQL
	t.Logf("SQL with include: %s", sql)
	// Should contain the fragment content
	if sql == "" {
		t.Error("SQL should contain the included fragment")
	}
}

// parseXMLString parses a single in-memory mapper and returns the single statement's
// TestSQL (OldSQL after parameter substitution) with runs of whitespace collapsed to
// single spaces, so assertions focus on SQL semantics (connectors, parens, values)
// rather than source newline/indent formatting.
func parseXMLString(t *testing.T, xmlContent string) string {
	t.Helper()
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "t.xml"), []byte(xmlContent), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := ParseDirectory(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(res.Statements))
	}
	got := sqlpkg.ExtractAndReplace(res.Statements[0].OldSQL)
	return strings.Join(strings.Fields(got), " ")
}

func TestIBatisDynamicWithIsNotEmpty(t *testing.T) {
	// dynamic prepend=AND + isNotEmpty prepend=AND, body self-carrying AND.
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<select id="s">
  select HOLIDAYDATE from eip.BANKHOLIDAY
  where 1=1
  <dynamic prepend="AND">
    <isNotEmpty prepend="AND" property="beginDate">
      and HOLIDAYDATE &gt;= STR_TO_DATE(#beginDate#, '%Y%m%d')
    </isNotEmpty>
    <isNotEmpty prepend="AND" property="endDate">
      and HOLIDAYDATE &lt;= STR_TO_DATE(#endDate#, '%Y%m%d')
    </isNotEmpty>
  </dynamic>
</select>`
	got := parseXMLString(t, xmlContent)
	want := "select HOLIDAYDATE from eip.BANKHOLIDAY where 1=1 AND HOLIDAYDATE >= STR_TO_DATE('20230302', '%Y%m%d') AND HOLIDAYDATE <= STR_TO_DATE('20230302', '%Y%m%d')"
	if got != want {
		t.Errorf("dynamic+isNotEmpty:\n got: %q\nwant: %q", got, want)
	}
}

func TestIBatisDynamicPrependWhere(t *testing.T) {
	// <dynamic prepend="where"> acts as the WHERE keyword.
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<select id="s">
  select a.acseq, b.cifseq from ac a, ac b
  <dynamic prepend="where">
    <isNotEmpty prepend="and" property="AcNo">a.acseq=b.acseq</isNotEmpty>
    <isNull prepend="and" property="X">b.acstate not in ('N')</isNull>
  </dynamic>
</select>`
	got := parseXMLString(t, xmlContent)
	want := "select a.acseq, b.cifseq from ac a, ac b where a.acseq=b.acseq and b.acstate not in ('N')"
	if got != want {
		t.Errorf("dynamic prepend=where:\n got: %q\nwant: %q", got, want)
	}
}

func TestIBatisBareIsWithoutDynamic(t *testing.T) {
	// R2 self-check #3: bare is* directly under a statement, body self-carries "and".
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<select id="s">
  select HOLIDAYDATE from eip.BANKHOLIDAY
  where 1=1
  <isNotEmpty prepend="and" property="beginDate">and HOLIDAYDATE &gt;= STR_TO_DATE(#b#, '%Y%m%d')</isNotEmpty>
  <isNull prepend="and" property="flag">and X is null</isNull>
</select>`
	got := parseXMLString(t, xmlContent)
	want := "select HOLIDAYDATE from eip.BANKHOLIDAY where 1=1 and HOLIDAYDATE >= STR_TO_DATE('20230302', '%Y%m%d') and X is null"
	if got != want {
		t.Errorf("bare is*:\n got: %q\nwant: %q", got, want)
	}
}

func TestIBatisIterateInParens(t *testing.T) {
	// <iterate> under an isNotEmpty for `in` — must produce in ('x') with parens.
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<select id="s">
  select count(*) from ecusrmch
  where (lastlogintime &lt;= STR_TO_DATE(#loginTime#, '%Y-%m-%d %H:%i:%s'))
  <dynamic prepend="AND">
    <isNotEmpty prepend="AND" property="channelIds">
      and mchannelid in
      <iterate open="(" close=")" conjunction=",">#channels[]#</iterate>
    </isNotEmpty>
  </dynamic>
</select>`
	got := parseXMLString(t, xmlContent)
	want := "select count(*) from ecusrmch where (lastlogintime <= STR_TO_DATE('20230302', '%Y-%m-%d %H:%i:%s')) AND mchannelid in ('20230302')"
	if got != want {
		t.Errorf("iterate in:\n got: %q\nwant: %q", got, want)
	}
}

func TestIBatisIterateNonInContext(t *testing.T) {
	// R3 self-check: iterate must produce open/value/close regardless of `in` context.
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<select id="s">
  select * from ac
  where id = <iterate open="(" close=")" conjunction=",">#ids[]#</iterate>
</select>`
	got := parseXMLString(t, xmlContent)
	want := "select * from ac where id = ('20230302')"
	if got != want {
		t.Errorf("iterate non-in:\n got: %q\nwant: %q", got, want)
	}
}

func TestIBatisIsEqualBodyWithoutAnd(t *testing.T) {
	// isEqual producing a single connector (no "AND AND").
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<select id="s">
  select mc.payeeacno from mc
  where 1=1
  <dynamic prepend="and">
    <isEqual prepend="and" compareValue="ent.MCSingleUpTransfer" property="Prdid">mc.payeeacno=ec.acno</isEqual>
    <isNotEqual prepend="and" compareValue="ent.MCSingleUpTransfer" property="Prdid">mc.payeracno=ec.acno</isNotEqual>
  </dynamic>
</select>`
	got := parseXMLString(t, xmlContent)
	want := "select mc.payeeacno from mc where 1=1 and mc.payeeacno=ec.acno and mc.payeracno=ec.acno"
	if got != want {
		t.Errorf("isEqual/isNotEqual:\n got: %q\nwant: %q", got, want)
	}
}

func TestIBatisCommaPrependDedup(t *testing.T) {
	// prepend="," must join without doubling; <dynamic prepend="set"> supplies the SET
	// keyword to the first clause, subsequent clauses use their own "," prepend.
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<update id="u">
  update t
  <dynamic prepend="set">
    <isNotEmpty prepend="," property="x">x=#x#</isNotEmpty>
    <isNotEmpty prepend="," property="y">y=#y#</isNotEmpty>
  </dynamic>
  where id=1
</update>`
	got := parseXMLString(t, xmlContent)
	want := "update t set x='20230302', y='20230302' where id=1"
	if got != want {
		t.Errorf("comma/set dedup:\n got: %q\nwant: %q", got, want)
	}
}

func TestIBatisDynamicNestedChooseAndSet(t *testing.T) {
	// R5: <dynamic> must compose with existing <choose>/<when>/<otherwise> and <set>
	// branches without connector duplication or lost fragments.
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<select id="s">
  select * from ac
  where 1=1
  <dynamic prepend="and">
    <isNotEmpty prepend="and" property="k">k=#k#</isNotEmpty>
    <choose>
      <when test="sort != null">order by a.transTime desc</when>
      <otherwise>order by a.cifseq</otherwise>
    </choose>
  </dynamic>
</select>`
	got := parseXMLString(t, xmlContent)
	if !strings.Contains(got, "where 1=1 and k='20230302'") {
		t.Errorf("nested choose dynamic: missing k condition: %q", got)
	}
	if !strings.Contains(got, "order by a.cifseq") {
		t.Errorf("nested choose dynamic: otherwise branch missing: %q", got)
	}
}

func TestIBatisDynamicSetNested(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<update id="u">
  update t
  <dynamic prepend="set">
    <isNotEmpty prepend="," property="a">a=#a#</isNotEmpty>
    <choose>
      <when test="b != null">, b=#b#</when>
      <otherwise>c=c</otherwise>
    </choose>
  </dynamic>
  where id=1
</update>`
	got := parseXMLString(t, xmlContent)
	if strings.Contains(got, "set set") || strings.Contains(got, ",,") {
		t.Errorf("nested set duplication: %q", got)
	}
	if !strings.Contains(got, "set a='20230302'") {
		t.Errorf("nested set first clause missing: %q", got)
	}
}

func TestIBatisBatchIterateSetFragment(t *testing.T) {
	// Batch UPDATE structure 1: <iterate> wraps only the SET assignment fragment
	// (conjunction=";"), body is SQL that must survive, not be swallowed.
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<update id="batchUpdateEcUsrMch">
  update ec_usr_mch set
  <iterate property="MapList" conjunction=";">
    HMAC=#MapList[].hmac#, USERSEQ=#MapList[].userSeq#
  </iterate>
  where MCHANNELID=#mchannelid#
</update>`
	got := parseXMLString(t, xmlContent)
	want := "update ec_usr_mch set HMAC='20230302', USERSEQ='20230302' where MCHANNELID='20230302'"
	if got != want {
		t.Errorf("batch set-fragment iterate:\n got: %q\nwant: %q", got, want)
	}
}

func TestIBatisBatchIterateWholeStatement(t *testing.T) {
	// Batch UPDATE structure 2: <iterate> wraps a whole statement
	// (conjunction=";"); expands to a single representative UPDATE.
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<update id="batchUpdateEcUsrRole">
  <iterate property="roleList" conjunction=";">
    update ec_usr_role set HMAC=#roleList[].hmac# where USERSEQ=#roleList[].userSeq#
  </iterate>
</update>`
	got := parseXMLString(t, xmlContent)
	want := "update ec_usr_role set HMAC='20230302' where USERSEQ='20230302'"
	if got != want {
		t.Errorf("batch whole-statement iterate:\n got: %q\nwant: %q", got, want)
	}
}
