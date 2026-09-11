// Package xname contains the shared user-name input and legacy conversion rules.
package xname

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxLength = 100

// Normalize validates a user-supplied display name without changing its internal order or spaces.
func Normalize(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("name is required")
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxLength {
		return "", fmt.Errorf("name must contain at most %d characters", MaxLength)
	}
	return value, nil
}

// FromLegacy converts split names using the ordering previously used by the UI.
// Use only for migration or providers that do not supply a complete name.
func FromLegacy(firstName, lastName string) string {
	first := strings.TrimSpace(firstName)
	last := strings.TrimSpace(lastName)
	if first == "" {
		return last
	}
	if last == "" {
		return first
	}
	for _, r := range first + last {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return last + first
		}
	}
	return first + " " + last
}
