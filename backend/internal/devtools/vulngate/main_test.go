package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A trimmed govulncheck -format json stream: one called stdlib finding (twice,
// through two traces), one called module finding, one imported-only finding.
const stream = `{"config":{"protocol_version":"v1.0.0"}}
{"finding":{"osv":"GO-2026-0001","fixed_version":"v1.26.9","trace":[{"module":"stdlib","version":"v1.26.0","package":"net/http","function":"Serve"}]}}
{"finding":{"osv":"GO-2026-0001","fixed_version":"v1.26.9","trace":[{"module":"stdlib","version":"v1.26.0","package":"net/http","function":"Serve"},{"module":"github.com/todayz/backend"}]}}
{"finding":{"osv":"GO-2026-0002","fixed_version":"v1.26.10","trace":[{"module":"stdlib","version":"v1.26.0","package":"net/url","function":"Parse"}]}}
{"finding":{"osv":"GO-2026-0003","fixed_version":"v0.60.0","trace":[{"module":"golang.org/x/net","version":"v0.59.0","package":"golang.org/x/net/http2","function":"Error"}]}}
{"finding":{"osv":"GO-2026-0004","fixed_version":"v0.4.1","trace":[{"module":"github.com/moby/sys/user","version":"v0.4.0"}]}}
`

func TestParseKeepsCalledFindingsOnce(t *testing.T) {
	v, err := Parse(strings.NewReader(stream))
	require.NoError(t, err)
	require.Len(t, v, 3)
	require.Equal(t, "golang.org/x/net", v[0].Module)
	require.True(t, v[1].Stdlib())
	require.True(t, v[2].Stdlib())
}

func TestVerdict(t *testing.T) {
	v, err := Parse(strings.NewReader(stream))
	require.NoError(t, err)

	var out bytes.Buffer
	require.True(t, Verdict(v, false, &out), "a module finding always fails")
	require.Contains(t, out.String(), "vulnerable module: GO-2026-0003  golang.org/x/net@v0.59.0  fixed in v0.60.0")
	require.Contains(t, out.String(), "upgrade Go to 1.26.10 or later", "the highest fix wins (numeric, not lexical)")

	std := []Vuln{v[1], v[2]}
	out.Reset()
	require.False(t, Verdict(std, false, &out), "stdlib only warns by default")
	require.Contains(t, out.String(), "warning: 2 standard library")
	out.Reset()
	require.True(t, Verdict(std, true, &out))
	require.Contains(t, out.String(), "error: 2 standard library")

	out.Reset()
	require.False(t, Verdict(nil, true, &out))
	require.Contains(t, out.String(), "no called vulnerabilities")
}

func TestBadInput(t *testing.T) {
	_, err := Parse(strings.NewReader(`{"finding":`))
	require.Error(t, err)
}
