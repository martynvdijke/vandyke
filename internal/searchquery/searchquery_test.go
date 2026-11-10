package searchquery

import (
	"reflect"
	"testing"
)

func texts(phrases []Phrase) []string {
	if len(phrases) == 0 {
		return nil
	}
	out := make([]string, 0, len(phrases))
	for _, p := range phrases {
		out = append(out, p.Text)
	}
	return out
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		include []string
		exclude []string
	}{
		{name: "empty", input: ""},
		{name: "whitespace only", input: "   \t\n "},
		{name: "bare terms are required", input: "feature euten", include: []string{"feature", "euten"}},
		{name: "quoted phrase", input: `"van Dijke"`, include: []string{"van Dijke"}},
		{name: "phrase with bare term", input: `"van Dijke" feature`, include: []string{"van Dijke", "feature"}},
		{name: "excluded term", input: "feature -feutenreus", include: []string{"feature"}, exclude: []string{"feutenreus"}},
		{name: "excluded phrase", input: `feature -"van Dijke"`, include: []string{"feature"}, exclude: []string{"van Dijke"}},
		{name: "only exclusions", input: "-feature -euten", exclude: []string{"feature", "euten"}},
		{name: "unmatched quote is literal", input: `"feature`, include: []string{`"feature`}},
		{name: "unmatched quote after term", input: `feature "euten`, include: []string{"feature", `"euten`}},
		{name: "empty phrase is dropped", input: `"" feature`, include: []string{"feature"}},
		{name: "punctuation only", input: "%", include: []string{"%"}},
		{name: "lone dash is dropped", input: "-", include: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := Parse(tt.input)
			if got := texts(q.Include); !reflect.DeepEqual(got, tt.include) {
				t.Errorf("include = %#v, want %#v", got, tt.include)
			}
			if got := texts(q.Exclude); !reflect.DeepEqual(got, tt.exclude) {
				t.Errorf("exclude = %#v, want %#v", got, tt.exclude)
			}
		})
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "single term", input: "feature", want: `("feature")`},
		{name: "two terms", input: "feature euten", want: `("feature" AND "euten")`},
		{name: "phrase", input: `"van Dijke"`, want: `("van Dijke")`},
		{name: "exclusion", input: "feature -feutenreus", want: `(("feature") NOT "feutenreus")`},
		{name: "two exclusions", input: "feature -euten -boeke", want: `((("feature") NOT "euten") NOT "boeke")`},
		{name: "short term falls back", input: "qs", want: ""},
		{name: "short term among long ones falls back", input: "feature qs", want: ""},
		{name: "short exclusion falls back", input: "feature -qs", want: ""},
		{name: "short word inside a phrase falls back", input: `"de fiets"`, want: ""},
		{name: "only exclusions fall back", input: "-feature", want: ""},
		{name: "quotes are escaped", input: `a"b`, want: `("a""b")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Parse(tt.input).Match(); got != tt.want {
				t.Errorf("Match(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLikePatterns(t *testing.T) {
	include, exclude := Parse(`100% -"a_b"`).LikePatterns()
	if want := []string{`%100\%%`}; !reflect.DeepEqual(include, want) {
		t.Errorf("include = %#v, want %#v", include, want)
	}
	if want := []string{`%a\_b%`}; !reflect.DeepEqual(exclude, want) {
		t.Errorf("exclude = %#v, want %#v", exclude, want)
	}
}

func TestLikePatternsKeepBackslashesLiteral(t *testing.T) {
	include, _ := Parse(`a\b`).LikePatterns()
	if want := []string{`%a\\b%`}; !reflect.DeepEqual(include, want) {
		t.Errorf("include = %#v, want %#v", include, want)
	}
}
