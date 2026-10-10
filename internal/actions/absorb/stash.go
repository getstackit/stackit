package absorb

import "strings"

func findStashRef(stashList, marker string) string {
	for _, entry := range parseStashList(stashList) {
		if strings.Contains(entry.Message, marker) {
			return entry.Ref
		}
	}
	return ""
}

func findStashRefByMarkers(stashList string, markers ...string) string {
	for _, entry := range parseStashList(stashList) {
		for _, marker := range markers {
			if strings.Contains(entry.Message, marker) {
				return entry.Ref
			}
		}
	}
	return ""
}

type stashEntry struct {
	Ref     string
	Message string
}

func parseStashList(stashList string) []stashEntry {
	entries := []stashEntry{}
	for line := range strings.SplitSeq(stashList, "\n") {
		ref, message, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		entries = append(entries, stashEntry{
			Ref:     strings.TrimSpace(ref),
			Message: strings.TrimSpace(message),
		})
	}
	return entries
}
