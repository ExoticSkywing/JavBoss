package jav

import (
	"strings"
	"unicode"
)

// ActressNameParts is the canonical representation of one actress label
// returned by a metadata provider. Providers occasionally put a former/stage
// name in parentheses; that text is an alias and must not create another
// actress entity.
type ActressNameParts struct {
	Primary string
	Aliases []string
}

// ParseActressName splits a provider actress label into its primary name and
// parenthesised aliases. Both ASCII and full-width parentheses are accepted.
// The returned values are display-ready (the original spelling is preserved).
func ParseActressName(raw string) ActressNameParts {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ActressNameParts{}
	}

	runes := []rune(raw)
	primaryEnd := len(runes)
	for i, r := range runes {
		if r == '(' || r == '（' {
			primaryEnd = i
			break
		}
	}
	primary := strings.TrimSpace(string(runes[:primaryEnd]))

	aliases := make([]string, 0, 2)
	seen := make(map[string]struct{})
	addAlias := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || value == primary {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		aliases = append(aliases, value)
	}

	for i := primaryEnd; i < len(runes); {
		open := runes[i]
		if open != '(' && open != '（' {
			i++
			continue
		}
		close := ')'
		if open == '（' {
			close = '）'
		}
		start := i + 1
		end := start
		for end < len(runes) && runes[end] != close {
			end++
		}
		if end < len(runes) {
			for _, part := range strings.FieldsFunc(string(runes[start:end]), func(r rune) bool {
				return r == ',' || r == '，' || r == '、' || r == '/' || r == '／' || r == '|' || r == '｜' || r == ';' || r == '；'
			}) {
				addAlias(part)
			}
			i = end + 1
			continue
		}
		// An unmatched opening parenthesis is treated as ordinary text. This
		// keeps malformed provider output from silently losing the name.
		i++
	}

	if primary == "" && len(aliases) > 0 {
		primary = aliases[0]
		aliases = aliases[1:]
	}
	if primary == "" {
		primary = raw
	}
	if len(aliases) == 0 {
		aliases = nil
	}
	return ActressNameParts{Primary: primary, Aliases: aliases}
}

// IsJapaneseName reports whether a name contains a Japanese-script rune. It
// is intentionally conservative: it is used only to choose a canonical
// display name, never as proof that two people are the same.
func IsJapaneseName(value string) bool {
	for _, r := range strings.TrimSpace(value) {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			return true
		case r >= 0x31F0 && r <= 0x31FF:
			return true
		case r >= 0x4E00 && r <= 0x9FFF:
			return true
		case r >= 0xFF66 && r <= 0xFF9D:
			return true
		}
	}
	return false
}
