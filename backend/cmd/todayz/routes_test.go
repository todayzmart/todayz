package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/jobs"
)

func mountName(domain string) string {
	if domain == "ai" {
		return "mountAI"
	}
	return "mount" + strings.ToUpper(domain[:1]) + domain[1:]
}

// TestOneMountPerDomain (D-1): every domain in the jobs registry has its own
// routes_<domain>.go defining mountX, and routes.go calls each exactly once,
// so two domain work packages never edit the same file.
func TestOneMountPerDomain(t *testing.T) {
	routes, err := os.ReadFile("routes.go")
	require.NoError(t, err)
	files, err := filepath.Glob("routes_*.go")
	require.NoError(t, err)
	want := map[string]bool{"routes_test.go": true}
	for _, d := range jobs.Domains {
		name := mountName(d.Name)
		file := "routes_" + d.Name + ".go"
		want[file] = true
		b, err := os.ReadFile(file)
		require.NoError(t, err, d.Name)
		require.Contains(t, string(b), "func "+name+"(r chi.Router, d *deps)", file)
		require.Equal(t, 1, strings.Count(string(routes), "\t"+name+"(r, &d)\n"), "routes.go calls %s once", name)
	}
	for _, f := range files {
		require.True(t, want[f], "%s is not a domain's routes file", f)
	}
	require.Len(t, jobs.Domains, 25)
}
