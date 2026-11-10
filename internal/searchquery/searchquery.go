// Package searchquery parses the ledger's small search grammar into an FTS5
// expression or a set of LIKE patterns. One parser serves both the SQL builder
// and the highlighter, so the two cannot disagree about what was searched.
package searchquery

import (
	"strings"
	"unicode"
)

// MinFTSTermRunes is the trigram tokenizer's minimum usable term length. A
// query containing a shorter term is answered by the LIKE fallback instead.
const MinFTSTermRunes = 3

// Phrase is one search term: a bare word or the contents of a quoted phrase.
type Phrase struct {
	Text string
}

// Query is a parsed search. Every Include phrase must match and no Exclude
// phrase may match.
type Query struct {
	Include []Phrase
	Exclude []Phrase
}

// Empty reports whether the input contained no searchable term at all.
func (q Query) Empty() bool {
	return len(q.Include) == 0 && len(q.Exclude) == 0
}

// Parse splits input on whitespace into required terms. A double-quoted run is
// one phrase, a leading '-' excludes the following term or phrase, and an
// unmatched quote is taken literally. Empty terms are dropped.
func Parse(input string) Query {
	var q Query
	runes := []rune(input)
	for i := 0; i < len(runes); {
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		if i >= len(runes) {
			break
		}

		exclude := false
		if runes[i] == '-' {
			exclude = true
			i++
			if i >= len(runes) {
				// A lone '-' is punctuation, not a term.
				break
			}
		}

		var text string
		if runes[i] == '"' {
			if end := closingQuote(runes, i+1); end >= 0 {
				text = string(runes[i+1 : end])
				i = end + 1
			} else {
				// Unmatched quote: keep it as an ordinary character.
				text, i = scanBare(runes, i)
			}
		} else {
			text, i = scanBare(runes, i)
		}

		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if exclude {
			q.Exclude = append(q.Exclude, Phrase{Text: text})
		} else {
			q.Include = append(q.Include, Phrase{Text: text})
		}
	}
	return q
}

// closingQuote returns the index of the next double quote at or after start,
// or -1 when the phrase is not terminated.
func closingQuote(runes []rune, start int) int {
	for i := start; i < len(runes); i++ {
		if runes[i] == '"' {
			return i
		}
	}
	return -1
}

// scanBare consumes characters up to the next whitespace.
func scanBare(runes []rune, start int) (string, int) {
	i := start
	for i < len(runes) && !unicode.IsSpace(runes[i]) {
		i++
	}
	return string(runes[start:i]), i
}

// Match returns an FTS5 expression for the query, or "" when the query cannot
// be answered by the trigram index: no positive term, or any term (positive or
// negative, including a word inside a phrase) shorter than three runes.
func (q Query) Match() string {
	if len(q.Include) == 0 {
		return ""
	}
	for _, phrase := range q.Include {
		if !ftsSafe(phrase.Text) {
			return ""
		}
	}
	for _, phrase := range q.Exclude {
		if !ftsSafe(phrase.Text) {
			return ""
		}
	}

	included := make([]string, 0, len(q.Include))
	for _, phrase := range q.Include {
		included = append(included, quoteFTS(phrase.Text))
	}
	expr := "(" + strings.Join(included, " AND ") + ")"
	for _, phrase := range q.Exclude {
		expr = "(" + expr + " NOT " + quoteFTS(phrase.Text) + ")"
	}
	return expr
}

// LikePatterns returns the substring patterns for the LIKE fallback, already
// escaped for use with ESCAPE '\'. The caller still binds them as parameters.
func (q Query) LikePatterns() (include, exclude []string) {
	for _, phrase := range q.Include {
		include = append(include, "%"+escapeLike(phrase.Text)+"%")
	}
	for _, phrase := range q.Exclude {
		exclude = append(exclude, "%"+escapeLike(phrase.Text)+"%")
	}
	return include, exclude
}

// ftsSafe reports whether every word in text can be handed to the trigram
// tokenizer. A phrase is tokenized word by word, so one short word is enough to
// force the LIKE fallback.
func ftsSafe(text string) bool {
	words := strings.Fields(text)
	if len(words) == 0 {
		return false
	}
	for _, word := range words {
		if len([]rune(word)) < MinFTSTermRunes {
			return false
		}
	}
	return true
}

// quoteFTS wraps text in an FTS5 string literal, doubling embedded quotes so
// the expression cannot be broken out of.
func quoteFTS(text string) string {
	return `"` + strings.ReplaceAll(text, `"`, `""`) + `"`
}

// escapeLike escapes the LIKE metacharacters and the escape character itself.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
