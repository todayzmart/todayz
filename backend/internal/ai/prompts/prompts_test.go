package prompts

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	p, err := Parse("x/system", []byte("version: 3\n---\nRead the note for {{.Store}}.\n"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(p.Version, "3-"))
	assert.Len(t, p.Version, len("3-")+8)
	s, err := p.Render(map[string]string{"Store": "Ravi Kirana"})
	require.NoError(t, err)
	assert.Equal(t, "Read the note for Ravi Kirana.", s)

	_, err = p.Render(map[string]string{})
	assert.Error(t, err, "a missing value is an error, never <no value>")

	// The body is in the version: an edit without a bump still changes it.
	q, err := Parse("x/system", []byte("version: 3\n---\nRead the note for {{.Store}}!\n"))
	require.NoError(t, err)
	assert.NotEqual(t, p.Version, q.Version)

	for name, src := range map[string]string{
		"no header":  "Read the note.",
		"version 0":  "version: 0\n---\nx",
		"no rule":    "version: 1\nx",
		"empty body": "version: 1\n---\n  \n",
		"bad tmpl":   "version: 1\n---\n{{.X",
	} {
		_, err := Parse("x/y", []byte(src))
		assert.ErrorIs(t, err, ErrFormat, name)
	}
}

func TestRegistry(t *testing.T) {
	MustRegister("prompts_test", fstest.MapFS{
		"system.md": {Data: []byte("version: 2\n---\nSystem.")},
		"user.md":   {Data: []byte("version: 1\n---\nUser {{.N}}.")},
		"notes.txt": {Data: []byte("not a prompt")},
	})
	sys := MustGet("prompts_test/system")
	usr, ok := Get("prompts_test/user")
	require.True(t, ok)
	assert.Equal(t, sys.Version+"+"+usr.Version, Version("prompts_test"))
	_, ok = Get("prompts_test/notes")
	assert.False(t, ok)

	assert.Panics(t, func() {
		MustRegister("prompts_test", fstest.MapFS{"system.md": {Data: []byte("version: 2\n---\nSystem.")}})
	})
	assert.Panics(t, func() { MustRegister("Bad Name", fstest.MapFS{"a.md": {Data: []byte("version: 1\n---\na")}}) })
	assert.Panics(t, func() { MustRegister("empty_feature", fstest.MapFS{}) })
	assert.Panics(t, func() { MustRegister("bad_file", fstest.MapFS{"a.md": {Data: []byte("a")}}) })
	assert.Panics(t, func() { MustGet("prompts_test/none") })
}
