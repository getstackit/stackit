package git

// ShortRevision returns a shortened version of a git revision hash.
// Returns at most maxLen characters, defaulting to 7 if maxLen <= 0.
func ShortRevision(rev string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 7
	}
	return rev[:min(maxLen, len(rev))]
}
