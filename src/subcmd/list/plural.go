package list

import (
	"fmt"
	"strings"
)

// These are included because they are common technical terms.
var (
	specialPlurals = map[string]string{
		"index":  "indices",
		"matrix": "matrices",
		"vertex": "vertices",
	}

	sibilantEndings = []string{"s", "sh", "tch", "x"}

	isVowel = map[byte]bool{
		'A': true, 'E': true, 'I': true, 'O': true, 'U': true,
		'a': true, 'e': true, 'i': true, 'o': true, 'u': true,
	}
)

func Plural(quantity int, singular, plural string) string {
	return fmt.Sprintf("%d %s", quantity, PluralWord(quantity, singular, plural))
}

// PluralWord builds the plural form of an English word.
// The simple English rules of regular pluralization will be used
// if the plural form is an empty string (i.e. not explicitly given).
// The special cases are not guaranteed to work for strings outside ASCII.
func PluralWord(quantity int, singular, plural string) string {
	if quantity == 1 {
		return singular
	}
	if plural != "" {
		return plural
	}
	if plural = specialPlurals[singular]; plural != "" {
		return plural
	}

	// We need to guess what the English plural might be.  Keep this
	// function simple!  It doesn't need to know about every possiblity;
	// only regular rules and the most common special cases.
	//
	// Reference: http://en.wikipedia.org/wiki/English_plural

	for _, ending := range sibilantEndings {
		if strings.HasSuffix(singular, ending) {
			return singular + "es"
		}
	}
	l := len(singular)
	if l >= 2 && singular[l-1] == 'o' && !isVowel[singular[l-2]] {
		return singular + "es"
	}
	if l >= 2 && singular[l-1] == 'y' && !isVowel[singular[l-2]] {
		return singular[:l-1] + "ies"
	}

	return singular + "s"
}
