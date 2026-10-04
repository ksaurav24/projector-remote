package anime

import (
	"fmt"
	"strings"
	"unicode"
)

func normalized(s string) string {
	return strings.Map(func(r rune) rune {
		// Question/exclamation marks distinguish releases such as Kaguya-sama
		// season 2 and K-On!!. Keep these while ignoring cosmetic punctuation.
		if r == '？' {
			return '?'
		}
		if r == '！' {
			return '!'
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '?' || r == '!' {
			return unicode.ToLower(r)
		}
		return -1
	}, plain(s))
}

// Exact normalized aliases intentionally prefer a missed match over playing a
// sequel, recap or different season. Never use the first search result.
func matches(m Media, names ...string) bool {
	for _, a := range m.titles() {
		a = normalized(a)
		if a == "" {
			continue
		}
		for _, b := range names {
			if a == normalized(b) {
				return true
			}
		}
	}
	return false
}

type candidate struct {
	id     string
	titles []string
}

func uniqueMatch(m Media, candidates []candidate) (string, error) {
	found := ""
	for _, c := range candidates {
		if c.id != "" && matches(m, c.titles...) {
			if found != "" && found != c.id {
				return "", fmt.Errorf("ambiguous anime match")
			}
			found = c.id
		}
	}
	if found == "" {
		return "", fmt.Errorf("no exact anime title match")
	}
	return found, nil
}
