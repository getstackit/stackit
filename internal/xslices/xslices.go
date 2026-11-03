// Package xslices holds slice helpers the standard slices package lacks.
package xslices

// Unique returns items with duplicates removed, keeping the first occurrence of
// each value in its original order. The input is not modified.
func Unique[T comparable](items []T) []T {
	seen := make(map[T]struct{}, len(items))
	unique := make([]T, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		unique = append(unique, item)
	}
	return unique
}

// Set returns a membership set of items: every item maps to true. Lookups on
// the result are O(1), and a missing key reads as false.
func Set[T comparable](items []T) map[T]bool {
	set := make(map[T]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}
