package api

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestBuildCSP_HashesInlineScripts(t *testing.T) {
	t.Parallel()

	inline1 := `self.__next_f=self.__next_f||[];self.__next_f.push([0,"x"])`
	inline2 := `console.log("hi")`
	expected1 := sha256Hash(inline1)
	expected2 := sha256Hash(inline2)

	fs := fstest.MapFS{
		"index.html": {Data: []byte(`<html><head>` +
			`<script>` + inline1 + `</script>` +
			`<script src="/_next/static/chunks/main.js" async></script>` +
			`</head><body><script>` + inline2 + `</script></body></html>`)},
		"other.html": {Data: []byte(`<script>` + inline1 + `</script>`)}, // duplicate; should dedupe
		"asset.js":   {Data: []byte(`console.log("ignored")`)},           // not html; skipped
	}

	csp, err := buildCSP(fs)
	require.NoError(t, err)

	require.Contains(t, csp, "'sha256-"+expected1+"'")
	require.Contains(t, csp, "'sha256-"+expected2+"'")
	require.Contains(t, csp, "frame-ancestors 'none'")
	require.Contains(t, csp, "default-src 'self'")

	// Duplicate inline script should appear exactly once in the header.
	require.Equal(t, 1, strings.Count(csp, "'sha256-"+expected1+"'"))
}

func TestBuildCSP_NilFS(t *testing.T) {
	t.Parallel()

	csp, err := buildCSP(nil)
	require.NoError(t, err)
	require.Contains(t, csp, "script-src 'self';")
}

func TestBuildCSP_SkipsScriptsWithSrc(t *testing.T) {
	t.Parallel()

	// A <script src="..."> with content between the tags shouldn't produce a
	// hash (browser ignores the body when src is set, so hashing it would be
	// dead config that drifts with each build).
	fs := fstest.MapFS{
		"index.html": {Data: []byte(`<script src="/x.js">SHOULD_NOT_HASH</script>`)},
	}
	csp, err := buildCSP(fs)
	require.NoError(t, err)
	require.NotContains(t, csp, "SHOULD_NOT_HASH")
	require.NotContains(t, csp, sha256Hash("SHOULD_NOT_HASH"))
}

func TestBuildCSP_SkipsEmptyScripts(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"index.html": {Data: []byte(`<script></script><script>  </script>`)},
	}
	csp, err := buildCSP(fs)
	require.NoError(t, err)
	require.Equal(t, "default-src 'self'; "+
		"img-src 'self' data: https:; "+
		"style-src 'self' 'unsafe-inline'; "+
		"script-src 'self'; "+
		"connect-src 'self'; "+
		"frame-ancestors 'none'; "+
		"base-uri 'self'; "+
		"form-action 'self'", csp)
}

func sha256Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.StdEncoding.EncodeToString(sum[:])
}
