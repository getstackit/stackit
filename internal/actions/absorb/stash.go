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
	for _, line := range strings.Split(stashList, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		entries = append(entries, stashEntry{
			Ref:     strings.TrimSpace(parts[0]),
			Message: strings.TrimSpace(parts[1]),
		})
	}
	return entries
}
