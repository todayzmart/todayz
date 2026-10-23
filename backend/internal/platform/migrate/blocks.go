package migrate

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Block order (D-90, D-100 L5). Blocks merge in any order and goose applies
// them out of order, so a migration may only rely on what is certain to be in
// the database when it runs: the lower files of its own block (its WP merges
// them together) and every file of a lower block (the WPs it builds on merge
// first). CheckBlockOrder reads the Up section of each migration and reports a
// statement that touches a table (ALTER, DROP, INSERT, UPDATE, DELETE,
// TRUNCATE, CREATE INDEX, CREATE TRIGGER, CREATE POLICY, PARTITION OF, GRANT,
// REVOKE or COMMENT ON TABLE), references one (REFERENCES in CREATE TABLE or
// ALTER TABLE) or reads one from a view (FROM or JOIN in CREATE VIEW or CREATE
// MATERIALIZED VIEW), where the table is created by a higher block, by a later
// file of the same block, or by no migration at all. A view counts as created
// too, so a view over a view resolves.
//
// It reads statement heads after stripping comments, string literals and
// dollar-quoted bodies, so DDL a function builds at run time (ensure_partitions)
// is not seen; that is by design, those tables are listed in partition_policies.
// A table River's Go migration creates is not seen either: nothing may alter it.
//
// Out of its sight, on purpose (D-100 round 2, Low 14; db/migrations/README.md
// says the same): a statement that starts with WITH (a CTE INSERT, UPDATE or
// DELETE); the FROM and USING clauses of UPDATE and DELETE and any subselect
// (only the target table is read); SELECT ... INTO; the tables a view reads
// through a subselect or a function (only the names after FROM and JOIN,
// less the statement's own CTE names); ALTER TABLE ... ATTACH PARTITION (the parent is read, not the
// partition); CREATE RULE; LOCK TABLE; COPY; and anything inside a DO block or
// a function body. A migration that needs one of those on another block's
// table says so in a comment and keeps the dependency obvious.

type tableOrigin struct {
	file string
	num  int64
	lo   int64 // the first number of the file's block
}

const ident = `(?:public\.)?"?([A-Za-z_][A-Za-z0-9_]*)"?`

var (
	reCreateTable = regexp.MustCompile(`(?is)^create\s+(?:unlogged\s+)?table\s+(?:if\s+not\s+exists\s+)?` + ident)
	rePartitionOf = regexp.MustCompile(`(?is)\bpartition\s+of\s+` + ident)
	reTouches     = []*regexp.Regexp{
		regexp.MustCompile(`(?is)^alter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?` + ident),
		regexp.MustCompile(`(?is)^drop\s+table\s+(?:if\s+exists\s+)?` + ident),
		regexp.MustCompile(`(?is)^insert\s+into\s+` + ident),
		regexp.MustCompile(`(?is)^update\s+(?:only\s+)?` + ident),
		regexp.MustCompile(`(?is)^delete\s+from\s+(?:only\s+)?` + ident),
		regexp.MustCompile(`(?is)^truncate\s+(?:table\s+)?(?:only\s+)?` + ident),
		regexp.MustCompile(`(?is)^create\s+(?:unique\s+)?index\s+(?:concurrently\s+)?(?:if\s+not\s+exists\s+)?\S+\s+on\s+(?:only\s+)?` + ident),
		regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:constraint\s+)?trigger\s+\S+\s+(?:before|after|instead\s+of)\s+.*?\s+on\s+` + ident),
		regexp.MustCompile(`(?is)^comment\s+on\s+table\s+` + ident),
		regexp.MustCompile(`(?is)^(?:grant|revoke)\s+.*?\s+on\s+(?:table\s+)?` + ident),
		regexp.MustCompile(`(?is)^create\s+policy\s+\S+\s+on\s+` + ident),
	}
	// A view is created like a table for the purpose of the check (a view over a
	// view resolves) and reads the tables named after FROM and JOIN.
	reCreateView = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:materialized\s+)?view\s+(?:if\s+not\s+exists\s+)?` + ident)
	reFromJoin   = regexp.MustCompile(`(?is)\b(?:from|join)\s+` + ident + `(\s*\()?`)
	// name AS ( ... ) declares a CTE; a view reading from it reads no table.
	reCTE = regexp.MustCompile(`(?is)\b([A-Za-z_][A-Za-z0-9_]*)\s+as\s*\(`)
	// REFERENCES <table> inside CREATE TABLE or ALTER TABLE.
	reReferences = regexp.MustCompile(`(?is)\breferences\s+` + ident)
	reAlterTable = regexp.MustCompile(`(?is)^alter\s+table\b`)
	// A list after DROP TABLE, GRANT ... ON or REVOKE ... ON names several tables.
	reMoreNames = regexp.MustCompile(`(?is)^\s*,\s*` + ident)
	// What GRANT/REVOKE ... ON may name instead of a table.
	notATable = map[string]bool{
		"function": true, "procedure": true, "routine": true, "schema": true, "sequence": true,
		"database": true, "type": true, "domain": true, "foreign": true, "large": true,
		"language": true, "tablespace": true, "all": true, "parameter": true,
	}
	reLineComment = regexp.MustCompile(`--[^\n]*`)
	reBlockCmt    = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reString      = regexp.MustCompile(`'(?:[^']|'')*'`)
)

// CheckBlockOrder applies the D-90 rule to files, a map of migration file
// name to content. Every name must pass CheckFileName first.
func CheckBlockOrder(files map[string]string) error {
	type parsed struct {
		name       string
		num, lo    int64
		statements []string
	}
	all := make([]parsed, 0, len(files))
	for name, content := range files {
		m := reFileName.FindStringSubmatch(name)
		if m == nil {
			return fmt.Errorf("%s: not a migration file name (D-89)", name)
		}
		num, _ := strconv.ParseInt(m[1], 10, 64)
		wave, _ := strconv.Atoi(m[3])
		wp, _ := strconv.Atoi(m[4])
		lo, _ := Block(wave, wp)
		all = append(all, parsed{name: name, num: num, lo: lo, statements: upStatements(content)})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].num < all[j].num })

	created := map[string]tableOrigin{}
	for _, f := range all {
		for _, s := range f.statements {
			for _, re := range []*regexp.Regexp{reCreateTable, reCreateView} {
				if m := re.FindStringSubmatch(s); m != nil {
					t := strings.ToLower(m[1])
					if _, dup := created[t]; !dup {
						created[t] = tableOrigin{file: f.name, num: f.num, lo: f.lo}
					}
				}
			}
		}
	}

	var errs []error
	for _, f := range all {
		for _, s := range f.statements {
			for _, t := range touchedTables(s) {
				o, ok := created[t]
				switch {
				case !ok:
					errs = append(errs, fmt.Errorf("%s touches table %s, which no migration creates (D-90)", f.name, t))
				case o.lo > f.lo:
					errs = append(errs, fmt.Errorf("%s touches table %s, created by %s in a higher block; a migration may only touch tables of its own or a lower block (D-90)", f.name, t, o.file))
				case o.lo == f.lo && o.num > f.num:
					errs = append(errs, fmt.Errorf("%s touches table %s, created later in the same block by %s (D-90)", f.name, t, o.file))
				}
			}
		}
	}
	return errors.Join(errs...)
}

// touchedTables lists the tables one statement depends on, in lower case.
func touchedTables(s string) []string {
	var out []string
	add := func(name string) {
		name = strings.ToLower(name)
		if !notATable[name] {
			out = append(out, name)
		}
	}
	if m := reCreateTable.FindStringSubmatch(s); m != nil {
		// CREATE TABLE child PARTITION OF parent depends on the parent, and a
		// table on every table its columns and constraints reference.
		if p := rePartitionOf.FindStringSubmatch(s); p != nil {
			add(p[1])
		}
		for _, r := range reReferences.FindAllStringSubmatch(s, -1) {
			add(r[1])
		}
		return out
	}
	if m := reCreateView.FindStringSubmatch(s); m != nil {
		// A view reads the tables named after FROM and JOIN; a name followed by
		// `(` is a function call and a name the statement declares as a CTE is
		// not a table.
		cte := map[string]bool{}
		for _, c := range reCTE.FindAllStringSubmatch(s, -1) {
			cte[strings.ToLower(c[1])] = true
		}
		for _, r := range reFromJoin.FindAllStringSubmatch(s, -1) {
			if r[2] == "" && !cte[strings.ToLower(r[1])] {
				add(r[1])
			}
		}
		return out
	}
	if reAlterTable.MatchString(s) {
		for _, r := range reReferences.FindAllStringSubmatch(s, -1) {
			add(r[1])
		}
	}
	for _, re := range reTouches {
		loc := re.FindStringSubmatchIndex(s)
		if loc == nil {
			continue
		}
		add(s[loc[2]:loc[3]])
		// DROP TABLE a, b and GRANT/REVOKE ... ON a, b: take the rest of the list.
		rest := s[loc[1]:]
		for {
			m := reMoreNames.FindStringSubmatchIndex(rest)
			if m == nil {
				break
			}
			add(rest[m[2]:m[3]])
			rest = rest[m[1]:]
		}
		break
	}
	return out
}

// upStatements returns the statements of the +goose Up section with comments,
// string literals and dollar-quoted bodies removed, each trimmed.
func upStatements(content string) []string {
	up := content
	if i := strings.Index(up, "-- +goose Up"); i >= 0 {
		up = up[i:]
	}
	if i := strings.Index(up, "-- +goose Down"); i >= 0 {
		up = up[:i]
	}
	up = stripDollarQuoted(up)
	up = reBlockCmt.ReplaceAllString(up, " ")
	up = reLineComment.ReplaceAllString(up, " ")
	up = reString.ReplaceAllString(up, "''")
	var out []string
	for _, s := range strings.Split(up, ";") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// stripDollarQuoted blanks every $tag$ ... $tag$ body (function bodies, DO
// blocks), matching the closing tag to the opening one.
func stripDollarQuoted(s string) string {
	var b strings.Builder
	for {
		open := strings.IndexByte(s, '$')
		if open < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := open + 1
		for end < len(s) && (s[end] == '_' || s[end] >= 'a' && s[end] <= 'z' || s[end] >= 'A' && s[end] <= 'Z' || s[end] >= '0' && s[end] <= '9') {
			end++
		}
		if end >= len(s) || s[end] != '$' {
			// A lone $ (a positional parameter): keep it and move on.
			b.WriteString(s[:open+1])
			s = s[open+1:]
			continue
		}
		tag := s[open : end+1]
		closeAt := strings.Index(s[end+1:], tag)
		if closeAt < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:open])
		b.WriteString(" ")
		s = s[end+1+closeAt+len(tag):]
	}
}
