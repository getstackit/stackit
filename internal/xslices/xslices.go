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
