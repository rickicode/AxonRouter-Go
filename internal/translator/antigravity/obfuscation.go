package antigravity

import (
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// zwj is the Zero-Width Joiner used to break simple substring matching of
// competitor/client names without changing visible text.
const zwj = "\u200d"

// regexCache stores compiled case-insensitive regexes per configured word.
// The key space is bounded by the configured obfuscation word list.
var regexCache sync.Map // string -> *regexp.Regexp

// Obfuscate inserts an invisible Zero-Width Joiner after the first character
// of each occurrence of the configured sensitive words. It preserves the
// original case, length, and readability while preventing naive grepping by
// upstream providers.
//
// An empty word list leaves the text unchanged.
func Obfuscate(text string, words []string) string {
	if text == "" || len(words) == 0 {
		return text
	}
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		re := obfuscationRegex(word)
		text = re.ReplaceAllStringFunc(text, insertZWJ)
	}
	return text
}

func obfuscationRegex(word string) *regexp.Regexp {
	if cached, ok := regexCache.Load(word); ok {
		return cached.(*regexp.Regexp)
	}
	re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(word))
	actual, _ := regexCache.LoadOrStore(word, re)
	return actual.(*regexp.Regexp)
}

func insertZWJ(m string) string {
	if utf8.RuneCountInString(m) <= 1 {
		return m
	}
	r, size := utf8.DecodeRuneInString(m)
	return string(r) + zwj + m[size:]
}
