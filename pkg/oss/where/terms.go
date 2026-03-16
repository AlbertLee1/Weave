package where

import "strings"

// SplitTerms splits a search string into individual terms.
// It splits on whitespace and filters out empty strings.
func SplitTerms(s string) []string {
	parts := strings.Fields(s)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
