package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Series suggestions look each book up by title and author in Audible's public
// catalog (no account or key needed; the same source Audiobookshelf uses) and
// group the library by the series the catalog puts each book in, with its
// number in that series. Nothing is changed here - the series manager shows the
// groups and the user applies the ones they agree with through the usual
// series endpoints.

// audiobookSeriesMatch is one series a catalog entry belongs to. Key is the
// catalog's own id for the series, so two books land in the same group even if
// one listing spells the name slightly differently.
type audiobookSeriesMatch struct {
	Key      string
	Name     string
	Sequence *float64
}

// audiobookSeriesLookup finds the series a book belongs to. An empty result
// with no error means the catalog has the book as a standalone, or doesn't
// have it at all.
type audiobookSeriesLookup interface {
	LookupSeries(ctx context.Context, title, author string) ([]audiobookSeriesMatch, error)
}

const audibleCatalogBaseURL = "https://api.audible.com/1.0/catalog/products"

type audibleSeriesLookup struct {
	client  *http.Client
	baseURL string
}

func newAudibleSeriesLookup() *audibleSeriesLookup {
	// http.DefaultTransport is the public-only pinned transport (or the
	// configured egress proxy) - see pinDownloaderDefaultTransport.
	return &audibleSeriesLookup{client: &http.Client{Timeout: 15 * time.Second}, baseURL: audibleCatalogBaseURL}
}

type audibleCatalogResponse struct {
	Products []struct {
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
		Authors  []struct {
			Name string `json:"name"`
		} `json:"authors"`
		Series []struct {
			ASIN     string `json:"asin"`
			Title    string `json:"title"`
			Sequence string `json:"sequence"`
		} `json:"series"`
	} `json:"products"`
}

func (l *audibleSeriesLookup) LookupSeries(ctx context.Context, title, author string) ([]audiobookSeriesMatch, error) {
	query := url.Values{}
	query.Set("title", title)
	if author != "" {
		query.Set("author", author)
	}
	query.Set("num_results", "10")
	query.Set("products_sort_by", "Relevance")
	query.Set("response_groups", "series,contributors,product_desc")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, l.baseURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := l.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("audible catalog returned %s", response.Status)
	}
	var payload audibleCatalogResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode audible catalog: %w", err)
	}

	// Relevance order isn't proof it's the same book: a search for a book
	// the catalog doesn't carry still returns its nearest neighbours. Only a
	// listing whose title matches, and by the same author when we know who
	// that is, counts.
	wantTitle := normalizeBookTitle(title)
	for _, product := range payload.Products {
		if !bookTitlesMatch(wantTitle, normalizeBookTitle(product.Title)) {
			continue
		}
		if author != "" {
			names := make([]string, 0, len(product.Authors))
			for _, a := range product.Authors {
				names = append(names, a.Name)
			}
			if !authorsOverlap(author, names) {
				continue
			}
		}
		out := make([]audiobookSeriesMatch, 0, len(product.Series))
		for _, series := range product.Series {
			name := strings.TrimSpace(series.Title)
			if name == "" {
				continue
			}
			key := strings.TrimSpace(series.ASIN)
			if key == "" {
				key = strings.ToLower(name)
			}
			out = append(out, audiobookSeriesMatch{Key: key, Name: name, Sequence: parseSeriesSequence(series.Sequence)})
		}
		return out, nil
	}
	return nil, nil
}

var seriesSequenceNumber = regexp.MustCompile(`\d+(?:\.\d+)?`)

// parseSeriesSequence reads the catalog's book number ("2", "2.5", sometimes
// "1-3" for an omnibus, where the first number is the useful one). An empty or
// non-numeric one - common for umbrella series like "The Cosmere" - is nil.
func parseSeriesSequence(raw string) *float64 {
	match := seriesSequenceNumber.FindString(raw)
	if match == "" {
		return nil
	}
	value, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return nil
	}
	return &value
}

var (
	bookTitleNoise    = regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*(unabridged|abridged|audiobook|audio book|dramati[sz]ed|book\s*\d+|volume\s*\d+|vol\.?\s*\d+|#\s*\d+)[^\)\]]*[\)\]]`)
	bookTitleFileTail = regexp.MustCompile(`(?i)\.(mp3|m4b|m4a|aac|flac|ogg|opus|wav)$`)
)

// audiobookLookupTitle is the title to search for: the tagged title, or the
// file name without its extension, minus edition noise like "(Unabridged)".
func audiobookLookupTitle(record audiobookRecord) string {
	title := strings.TrimSpace(record.Title)
	if title == "" {
		title = bookTitleFileTail.ReplaceAllString(strings.TrimSpace(record.Filename), "")
		title = strings.NewReplacer("_", " ", ".", " ").Replace(title)
	}
	title = bookTitleNoise.ReplaceAllString(title, "")
	return strings.Join(strings.Fields(title), " ")
}

// normalizeBookTitle lowercases and keeps only letters and digits (as single
// spaced words), and drops a leading article, so "The Way of Kings: Book One"
// and "Way of Kings" compare by words. Apostrophes vanish rather than split a
// word, so "Caliban's War" and "Calibans War" are the same title.
func normalizeBookTitle(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r == '\'' || r == '’':
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	words := strings.Fields(b.String())
	if len(words) > 1 && (words[0] == "the" || words[0] == "a" || words[0] == "an") {
		words = words[1:]
	}
	return strings.Join(words, " ")
}

// bookTitlesMatch accepts the same title, or one that is the other plus a
// subtitle ("Dune" vs "Dune Book One of the Dune Chronicles") - but never a
// one-word prefix of a longer title unless the other is just that word.
func bookTitlesMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	return strings.HasPrefix(long, short+" ")
}

// authorsOverlap reports whether the book's author field (which may list
// several people, "Last, First", or include a narrator) shares a surname-ish
// word of three or more letters with any catalog author.
func authorsOverlap(author string, catalogAuthors []string) bool {
	words := map[string]bool{}
	for _, word := range strings.Fields(normalizeBookTitle(author)) {
		if len(word) >= 3 {
			words[word] = true
		}
	}
	if len(words) == 0 {
		return true
	}
	for _, name := range catalogAuthors {
		for _, word := range strings.Fields(normalizeBookTitle(name)) {
			if words[word] {
				return true
			}
		}
	}
	return false
}

// cachedSeriesLookup remembers answers so scanning a library again (or
// continuing a scan that ran out of time) doesn't re-ask the catalog about
// every book. Failures aren't cached, so they're retried next scan.
type cachedSeriesLookup struct {
	inner audiobookSeriesLookup
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	entries map[string]cachedSeriesLookupEntry
}

type cachedSeriesLookupEntry struct {
	matches []audiobookSeriesMatch
	at      time.Time
}

const audiobookSeriesLookupCacheTTL = 24 * time.Hour

func newCachedSeriesLookup(inner audiobookSeriesLookup) *cachedSeriesLookup {
	return &cachedSeriesLookup{inner: inner, ttl: audiobookSeriesLookupCacheTTL, now: time.Now, entries: map[string]cachedSeriesLookupEntry{}}
}

func seriesLookupCacheKey(title, author string) string {
	return normalizeBookTitle(title) + "\x00" + normalizeBookTitle(author)
}

// cached returns the remembered answer, if there is a fresh one.
func (c *cachedSeriesLookup) cached(title, author string) ([]audiobookSeriesMatch, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[seriesLookupCacheKey(title, author)]
	if !ok || c.now().Sub(entry.at) > c.ttl {
		return nil, false
	}
	return entry.matches, true
}

func (c *cachedSeriesLookup) LookupSeries(ctx context.Context, title, author string) ([]audiobookSeriesMatch, error) {
	if matches, ok := c.cached(title, author); ok {
		return matches, nil
	}
	matches, err := c.inner.LookupSeries(ctx, title, author)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.entries[seriesLookupCacheKey(title, author)] = cachedSeriesLookupEntry{matches: matches, at: c.now()}
	c.mu.Unlock()
	return matches, nil
}

// Per request, at most this many books go out to the catalog (cached answers
// are free), a few at a time, inside an overall deadline. The rest are counted
// as remaining and the client asks again to carry on.
const (
	audiobookSeriesSuggestBatch       = 60
	audiobookSeriesSuggestConcurrency = 4
	audiobookSeriesSuggestDeadline    = 40 * time.Second
)

type audiobookSeriesSuggestionBook struct {
	record   audiobookRecord
	sequence *float64
}

type audiobookSeriesSuggestion struct {
	key              string
	name             string
	existingSeriesID string
	books            []audiobookSeriesSuggestionBook
}

type audiobookSeriesSuggestResult struct {
	suggestions []audiobookSeriesSuggestion
	checked     int
	remaining   int
	failed      int
	notFound    int
}

// suggestAudiobookSeries looks up each book (only those not in a series yet,
// unless includeAssigned) and groups them by catalog series. A group is kept
// when it would change something: it matches no existing series, or it does
// and some of its books aren't in it at that number yet.
func suggestAudiobookSeries(ctx context.Context, lookup *cachedSeriesLookup, books []audiobookRecord, existing []audiobookSeries, includeAssigned bool) audiobookSeriesSuggestResult {
	var result audiobookSeriesSuggestResult
	type job struct {
		record        audiobookRecord
		title, author string
	}
	var jobs []job
	for _, record := range books {
		if record.SeriesID != "" && !includeAssigned {
			continue
		}
		title := audiobookLookupTitle(record)
		if title == "" {
			continue
		}
		jobs = append(jobs, job{record: record, title: title, author: strings.TrimSpace(record.Author)})
	}

	matchesByBook := make([][]audiobookSeriesMatch, len(jobs))
	done := make([]bool, len(jobs))
	var uncached []int
	for i, j := range jobs {
		if matches, ok := lookup.cached(j.title, j.author); ok {
			matchesByBook[i], done[i] = matches, true
			continue
		}
		uncached = append(uncached, i)
	}
	if len(uncached) > audiobookSeriesSuggestBatch {
		uncached = uncached[:audiobookSeriesSuggestBatch]
	}

	ctx, cancel := context.WithTimeout(ctx, audiobookSeriesSuggestDeadline)
	defer cancel()
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed int
	)
	work := make(chan int)
	for range audiobookSeriesSuggestConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				matches, err := lookup.LookupSeries(ctx, jobs[i].title, jobs[i].author)
				mu.Lock()
				if err != nil {
					if ctx.Err() == nil {
						failed++
						log.Printf("audiobook series lookup for %q failed: %v", jobs[i].title, err)
					}
				} else {
					matchesByBook[i], done[i] = matches, true
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, i := range uncached {
		select {
		case work <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(work)
	wg.Wait()

	existingByName := map[string]string{}
	for _, series := range existing {
		existingByName[normalizeBookTitle(series.Name)] = series.ID
	}
	groups := map[string]*audiobookSeriesSuggestion{}
	for i, j := range jobs {
		if !done[i] {
			result.remaining++
			continue
		}
		result.checked++
		match, ok := primarySeriesMatch(matchesByBook[i])
		if !ok {
			result.notFound++
			continue
		}
		group := groups[match.Key]
		if group == nil {
			group = &audiobookSeriesSuggestion{key: match.Key, name: match.Name, existingSeriesID: existingByName[normalizeBookTitle(match.Name)]}
			groups[match.Key] = group
		}
		group.books = append(group.books, audiobookSeriesSuggestionBook{record: j.record, sequence: match.Sequence})
	}
	result.failed = failed

	for _, group := range groups {
		if group.existingSeriesID != "" && !suggestionChangesSeries(group) {
			continue
		}
		sort.SliceStable(group.books, func(a, b int) bool {
			sa, sb := group.books[a].sequence, group.books[b].sequence
			if sa == nil || sb == nil {
				return sa != nil
			}
			return *sa < *sb
		})
		result.suggestions = append(result.suggestions, *group)
	}
	sort.Slice(result.suggestions, func(a, b int) bool {
		sa, sb := result.suggestions[a], result.suggestions[b]
		if len(sa.books) != len(sb.books) {
			return len(sa.books) > len(sb.books)
		}
		return strings.ToLower(sa.name) < strings.ToLower(sb.name)
	})
	return result
}

// primarySeriesMatch picks the series to file a book under. Catalog entries
// often list an umbrella series with no number ("The Cosmere") next to the
// real one ("The Stormlight Archive", 1); the numbered one is what ordering
// needs, so it wins. When the umbrella is numbered too ("Wizarding World
// Collection" 1 beside "Harry Potter" 1), the catalog still lists it first,
// so the last numbered one is taken.
func primarySeriesMatch(matches []audiobookSeriesMatch) (audiobookSeriesMatch, bool) {
	for i := len(matches) - 1; i >= 0; i-- {
		if matches[i].Sequence != nil {
			return matches[i], true
		}
	}
	if len(matches) > 0 {
		return matches[0], true
	}
	return audiobookSeriesMatch{}, false
}

func suggestionChangesSeries(group *audiobookSeriesSuggestion) bool {
	for _, book := range group.books {
		if book.record.SeriesID != group.existingSeriesID {
			return true
		}
		current, suggested := book.record.SeriesSequence, book.sequence
		if suggested != nil && (current == nil || *current != *suggested) {
			return true
		}
	}
	return false
}

// handleAudiobookSeriesSuggestions serves GET /api/audiobook-series/suggestions
// (permissionManageAudiobookSeries). ?scope=all also re-checks books that are
// already in a series; by default only unassigned books are looked up.
func (s *Server) handleAudiobookSeriesSuggestions(w http.ResponseWriter, r *http.Request) {
	if s.audiobooks == nil {
		http.Error(w, "audiobooks require a database", http.StatusNotImplemented)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session, _ := sessionFromContext(r.Context())
	if s.auth.enabled() && !hasPermission(session.Permissions, permissionManageAudiobookSeries) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "suggesting audiobook series requires the manage-audiobook-series permission"})
		return
	}
	if s.enforceRateLimit(w, r, s.writeLimiter, "audiobook-series-suggest") {
		return
	}

	var restrictToTags []string
	if s.auth.enabled() {
		restrictToTags = session.Permissions.AudiobookTagRestriction
	}
	items, err := s.audiobooks.ListAudiobooks(r.Context(), currentAudiobookUserID(r), audiobookListFilter{RestrictToTags: restrictToTags})
	if err != nil {
		log.Printf("list audiobooks for series suggestions failed: %v", err)
		http.Error(w, "failed to load audiobooks", http.StatusInternalServerError)
		return
	}
	existing, err := s.audiobooks.ListSeries(r.Context(), restrictToTags)
	if err != nil {
		log.Printf("list series for suggestions failed: %v", err)
		http.Error(w, "failed to load series", http.StatusInternalServerError)
		return
	}
	books := make([]audiobookRecord, 0, len(items))
	for _, item := range items {
		books = append(books, item.audiobookRecord)
	}

	result := suggestAudiobookSeries(r.Context(), s.audiobookSeriesLookupService(), books, existing, r.URL.Query().Get("scope") == "all")
	out := make([]map[string]any, 0, len(result.suggestions))
	for _, suggestion := range result.suggestions {
		entries := make([]map[string]any, 0, len(suggestion.books))
		for _, book := range suggestion.books {
			entry := map[string]any{
				"id":                  book.record.ID,
				"title":               book.record.Title,
				"filename":            book.record.Filename,
				"author":              book.record.Author,
				"sequence":            book.sequence,
				"current_series_id":   book.record.SeriesID,
				"current_series_name": book.record.SeriesName,
				"current_sequence":    book.record.SeriesSequence,
			}
			if book.record.CoverPath != "" {
				entry["cover_url"] = "/audiobook-cover/" + book.record.ID
			}
			entries = append(entries, entry)
		}
		out = append(out, map[string]any{
			"key":                suggestion.key,
			"name":               suggestion.name,
			"existing_series_id": suggestion.existingSeriesID,
			"books":              entries,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"suggestions": out,
		"checked":     result.checked,
		"remaining":   result.remaining,
		"failed":      result.failed,
		"not_found":   result.notFound,
	})
}

var (
	defaultAudiobookSeriesLookupOnce sync.Once
	defaultAudiobookSeriesLookup     *cachedSeriesLookup
)

func (s *Server) audiobookSeriesLookupService() *cachedSeriesLookup {
	if s.audiobookSeriesLookup != nil {
		return s.audiobookSeriesLookup
	}
	defaultAudiobookSeriesLookupOnce.Do(func() {
		defaultAudiobookSeriesLookup = newCachedSeriesLookup(newAudibleSeriesLookup())
	})
	return defaultAudiobookSeriesLookup
}
