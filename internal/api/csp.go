package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

// scriptTagRe captures the attribute list and inline body of every <script>
// tag. The (?s) flag lets `.` span newlines so multi-line scripts match.
var scriptTagRe = regexp.MustCompile(`(?s)<script([^>]*)>(.*?)</script>`)

// scriptHasSrcRe matches `src=` as a real attribute (preceded by start or
// whitespace) so we don't false-positive on `data-src=`, `nosrc=`, etc.
var scriptHasSrcRe = regexp.MustCompile(`(?:^|\s)src\s*=`)

// buildCSP returns the Content-Security-Policy header value derived from the
// embedded static site. It computes a sha256 hash for every inline <script>
// body found in *.html files under staticFS, so each Next build is allowed
// without a manual paste-in. External scripts (with a src= attribute) are
// covered by 'self' and contribute no hash.
func buildCSP(staticFS fs.FS) (string, error) {
	hashes, err := inlineScriptHashes(staticFS)
	if err != nil {
		return "", err
	}

	scriptSrc := "'self'"
	if len(hashes) > 0 {
		scriptSrc = "'self' " + strings.Join(hashes, " ")
	}

	return "default-src 'self'; " +
		"img-src 'self' data: https:; " +
		"style-src 'self' 'unsafe-inline'; " +
		"script-src " + scriptSrc + "; " +
		"connect-src 'self'; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'", nil
}

// inlineScriptHashes returns CSP-formatted sha256 hashes for every distinct
// inline <script> body across all HTML files in staticFS. Sorted for stable
// header output across runs.
func inlineScriptHashes(staticFS fs.FS) ([]string, error) {
	if staticFS == nil {
		return nil, nil
	}

	seen := make(map[string]struct{})
	err := fs.WalkDir(staticFS, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".html") {
			return nil
		}

		data, err := fs.ReadFile(staticFS, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		for _, m := range scriptTagRe.FindAllSubmatch(data, -1) {
			attrs, body := m[1], m[2]
			if scriptHasSrcRe.Match(attrs) {
				continue
			}
			if len(bytes.TrimSpace(body)) == 0 {
				continue
			}
			sum := sha256.Sum256(body)
			seen["'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'"] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Strings(out)
	return out, nil
}
