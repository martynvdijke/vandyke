package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"vandyke.thor.edu/vandyke/internal/store"
)

// wordsPerPage is the dictionary page size. A word row is one text line with no
// thumbnail, so a letter is skimmable in as few pages as possible.
const wordsPerPage = 200

// wordBuckets is the fixed rail: A–Z plus # for everything that does not start
// with an ASCII letter.
var wordBuckets = []string{
	"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M",
	"N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z", "#",
}

// wordBucket is one link in the A–Z rail. An empty bucket renders as inert text
// so the rail never reshuffles as the corpus grows.
type wordBucket struct {
	Letter string
	Count  int
	Active bool
	Empty  bool
	Href   string
}

// wordsPageData is the view model for /words: the rail, exactly one rendered
// bucket, its numbered pager and the photo-only note.
type wordsPageData struct {
	pageData
	Rail        []wordBucket
	Active      string
	Rows        []store.WordSummary
	BucketTotal int
	Page        int
	Pages       int
	PageLinks   []pageLink
	PrevURL     string
	NextURL     string
	PhotoOnly   int
	Empty       bool
}

// handleWords serves the A–Z index of every distinct misspelling. The index is
// grouped in SQL and bucketed here on the group's newest spelling.
func (w *Web) handleWords(rw http.ResponseWriter, r *http.Request) {
	index, err := w.store.WordIndex(r.Context())
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	buckets := make(map[string][]store.WordSummary, len(wordBuckets))
	counts := make(map[string]int, len(wordBuckets))
	for _, summary := range index.Words {
		letter := wordBucketLetter(summary.Word)
		buckets[letter] = append(buckets[letter], summary)
		counts[letter]++
	}

	query := r.URL.Query()
	_, letterGiven := query["letter"]
	_, pageGiven := query["page"]

	active := ""
	switch {
	case letterGiven:
		letter := strings.ToUpper(strings.TrimSpace(query.Get("letter")))
		if letter != "#" && (len(letter) != 1 || letter[0] < 'A' || letter[0] > 'Z') {
			w.renderErrorPage(rw,
				"Letter not found",
				"That is not a letter in the index. Pick a letter from the rail.")
			return
		}
		if len(buckets[letter]) == 0 {
			w.renderErrorPage(rw,
				"Letter not found",
				"No misspelling in the ledger starts with that letter.")
			return
		}
		active = letter
	default:
		// # wins when it holds rows, otherwise the first non-empty letter.
		if len(buckets["#"]) > 0 {
			active = "#"
		} else {
			for _, letter := range wordBuckets {
				if letter != "#" && len(buckets[letter]) > 0 {
					active = letter
					break
				}
			}
		}
	}

	page, pages := 1, 0
	rows := buckets[active]
	bucketTotal := len(rows)
	if bucketTotal > 0 {
		pages = (bucketTotal + wordsPerPage - 1) / wordsPerPage
		if raw := strings.TrimSpace(query.Get("page")); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 1 {
				page = n
			}
		}
		if page > pages {
			page = pages
		}
		start := (page - 1) * wordsPerPage
		end := start + wordsPerPage
		if end > bucketTotal {
			end = bucketTotal
		}
		rows = rows[start:end]
	}

	rail := make([]wordBucket, 0, len(wordBuckets))
	for _, letter := range wordBuckets {
		count := counts[letter]
		bucket := wordBucket{Letter: letter, Count: count, Active: letter == active && count > 0, Empty: count == 0}
		if count > 0 {
			bucket.Href = "/words?letter=" + url.QueryEscape(letter)
		}
		rail = append(rail, bucket)
	}

	data := wordsPageData{
		pageData: pageData{
			Umami:        w.umami(),
			Year:         time.Now().Year(),
			Title:        "Words · vanDyke",
			Description:  "An A–Z index of every distinct misspelling in the LANCo ledger, with how often it was written down and when it first and last appeared.",
			CanonicalURL: w.absoluteURL(r, "/words"),
		},
		Rail:        rail,
		Active:      active,
		Rows:        rows,
		BucketTotal: bucketTotal,
		Page:        page,
		Pages:       pages,
		PhotoOnly:   index.PhotoOnly,
		Empty:       len(index.Words) == 0,
	}
	// The letter and page variants are filtered views of the same dictionary;
	// the bare index is the only document worth indexing.
	if letterGiven || pageGiven {
		data.Robots = "noindex,follow"
	}
	data.PageLinks = buildPageLinks(page, pages, func(n int) string {
		return wordsPageURL(active, n)
	})
	if page > 1 {
		data.PrevURL = wordsPageURL(active, page-1)
	}
	if page < pages {
		data.NextURL = wordsPageURL(active, page+1)
	}
	w.render(rw, http.StatusOK, "words-page", data)
}

// wordsPageURL builds the canonical page URL for one bucket page. Page one
// omits the parameter so the pager's first link is the readable form.
func wordsPageURL(letter string, page int) string {
	values := url.Values{}
	values.Set("letter", letter)
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	return "/words?" + values.Encode()
}

// wordBucketLetter assigns a spelling to its rail bucket. ASCII letters fold to
// their upper-case letter; digits, punctuation, accented and non-Latin spellings
// all go to #, exactly as the design states.
func wordBucketLetter(word string) string {
	r, _ := utf8.DecodeRuneInString(word)
	upper := unicode.ToUpper(r)
	if upper >= 'A' && upper <= 'Z' {
		return string(upper)
	}
	return "#"
}
