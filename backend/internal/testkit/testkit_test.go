package testkit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// rePrivilegeStmt picks the statements that must be the same in the test
// databases and the local stack's: extensions, schema ownership and the app
// role's privileges.
var rePrivilegeStmt = regexp.MustCompile(`^(CREATE EXTENSION|ALTER SCHEMA|REVOKE|GRANT|ALTER DEFAULT PRIVILEGES)\b`)

func privilegeStmts(sql string) []string {
	var out []string
	for line := range strings.SplitSeq(sql, "\n") {
		line = strings.TrimSpace(line)
		if rePrivilegeStmt.MatchString(line) && !strings.HasPrefix(line, "REVOKE TEMPORARY") {
			out = append(out, line)
		}
	}
	return out
}

// TestMatchesLocalInitSQL: database.sql runs the same per-database statements,
// in the same order, as infra/local/postgres/init.sql's block for todayz_test,
// so tests see the privileges `make dev` has. (REVOKE TEMPORARY names the
// database there and runs per copy here.) Skipped once the repos split.
func TestMatchesLocalInitSQL(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "infra", "local", "postgres", "init.sql"))
	if os.IsNotExist(err) {
		t.Skip("infra/local is not next to backend/ (split repos)")
	}
	require.NoError(t, err)
	_, block, ok := strings.Cut(string(b), `\connect todayz_test`)
	require.True(t, ok, "init.sql has a todayz_test block")
	block, _, _ = strings.Cut(block, `\connect`)
	want := privilegeStmts(block)
	require.NotEmpty(t, want)
	require.Equal(t, want, privilegeStmts(databaseSQL))
}
