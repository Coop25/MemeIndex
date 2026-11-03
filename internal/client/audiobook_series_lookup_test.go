package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func floatPtr(v float64) *float64 { return &v }

// TestAudibleSeriesLookupPicksMatchingListing covers that a relevance result
// for a different book (or another author's book of the same name) is never
// used, and that the matching listing's series and number are read.
func TestAudibleSeriesLookupPicksMatchingListing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("title"); got != "Words of Radiance" {
			t.Errorf("title query = %q", got)
		}
		_, _ = w.Write([]byte(`{"products":[
			{"title":"Words of Radiance","authors":[{"name":"Someone Else"}],"series":[{"asin":"X","title":"Wrong","sequence":"9"}]},
			{"title":"Edgedancer","authors":[{"name":"Brandon Sanderson"}],"series":[{"asin":"B006K1RP8I","title":"The Stormlight Archive","sequence":"2.5"}]},
			{"title":"Words of Radiance","subtitle":"Book Two","authors":[{"name":"Brandon Sanderson"}],"series":[
				{"asin":"B0DMXTJ8WH","title":"The Cosmere","sequence":""},
				{"asin":"B006K1RP8I","title":"The Stormlight Archive","sequence":"2"}]}
		]}`))
	}))
	defer server.Close()

	lookup := &audibleSeriesLookup{client: server.Client(), baseURL: server.URL}
	matches, err := lookup.LookupSeries(context.Background(), "Words of Radiance", "Sanderson, Brandon")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d series, want 2: %+v", len(matches), matches)
	}
	primary, ok := primarySeriesMatch(matches)
	if !ok || primary.Name != "The Stormlight Archive" || primary.Sequence == nil || *primary.Sequence != 2 {
		t.Fatalf("primary series = %+v", primary)
	}
}

func TestAudiobookLookupTitleCleansEditionNoise(t *testing.T) {
	cases := []struct {
		record audiobookRecord
		want   string
	}{
		{audiobookRecord{Title: "The Way of Kings (Unabridged)"}, "The Way of Kings"},
		{audiobookRecord{Title: "Mistborn [Book 1]"}, "Mistborn"},
		{audiobookRecord{Filename: "Project_Hail_Mary.m4b"}, "Project Hail Mary"},
		{audiobookRecord{Title: "  Dune  ", Filename: "ignored.mp3"}, "Dune"},
		{audiobookRecord{Title: "We Are Legion (We Are Bob) (Unabridged)"}, "We Are Legion (We Are Bob)"},
	}
	for _, c := range cases {
		if got := audiobookLookupTitle(c.record); got != c.want {
			t.Errorf("audiobookLookupTitle(%+v) = %q, want %q", c.record, got, c.want)
		}
	}
}

func TestBookTitlesMatch(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"The Way of Kings", "Way of Kings", true},
		{"Dune", "Dune: Book One of the Dune Chronicles", true},
		{"Dune", "Dune Messiah", true}, // can't tell these apart by title alone; the author check and the user's review cover it
		{"Dune", "Dunes of Mars", false},
		{"Oathbringer", "Rhythm of War", false},
		{"Calibans War", "Caliban’s War", true},
		{"", "Anything", false},
	}
	for _, c := range cases {
		if got := bookTitlesMatch(normalizeBookTitle(c.a), normalizeBookTitle(c.b)); got != c.want {
			t.Errorf("bookTitlesMatch(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

type fakeSeriesLookup struct {
	calls   atomic.Int32
	results map[string][]audiobookSeriesMatch
	fail    map[string]bool
}

func (f *fakeSeriesLookup) LookupSeries(_ context.Context, title, _ string) ([]audiobookSeriesMatch, error) {
	f.calls.Add(1)
	if f.fail[title] {
		return nil, errors.New("catalog down")
	}
	return f.results[title], nil
}

func TestSuggestAudiobookSeriesGroupsAndOrders(t *testing.T) {
	stormlight := func(seq float64) []audiobookSeriesMatch {
		return []audiobookSeriesMatch{
			{Key: "cosmere", Name: "The Cosmere"},
			{Key: "stormlight", Name: "The Stormlight Archive", Sequence: floatPtr(seq)},
		}
	}
	fake := &fakeSeriesLookup{
		results: map[string][]audiobookSeriesMatch{
			"Oathbringer":       stormlight(3),
			"The Way of Kings":  stormlight(1),
			"Words of Radiance": stormlight(2),
			"Mistborn":          {{Key: "mistborn", Name: "Mistborn", Sequence: floatPtr(1)}},
			"Elantris":          nil,
		},
		fail: map[string]bool{"Warbreaker": true},
	}
	lookup := newCachedSeriesLookup(fake)
	books := []audiobookRecord{
		{ID: "oath", Title: "Oathbringer"},
		{ID: "wok", Title: "The Way of Kings"},
		{ID: "wor", Title: "Words of Radiance"},
		// Already filed exactly where the catalog would put it.
		{ID: "mist", Title: "Mistborn", SeriesID: "s-mist", SeriesSequence: floatPtr(1)},
		{ID: "elan", Title: "Elantris"},
		{ID: "war", Title: "Warbreaker"},
	}
	existing := []audiobookSeries{{ID: "s-mist", Name: "mistborn"}}

	result := suggestAudiobookSeries(context.Background(), lookup, books, existing, true)
	if len(result.suggestions) != 1 {
		t.Fatalf("got %d suggestions, want only Stormlight: %+v", len(result.suggestions), result.suggestions)
	}
	got := result.suggestions[0]
	if got.name != "The Stormlight Archive" || got.existingSeriesID != "" {
		t.Fatalf("suggestion = %q (existing %q)", got.name, got.existingSeriesID)
	}
	var order []string
	for _, book := range got.books {
		order = append(order, book.record.ID)
	}
	if strings.Join(order, ",") != "wok,wor,oath" {
		t.Fatalf("book order = %v, want wok,wor,oath", order)
	}
	if result.checked != 5 || result.failed != 1 || result.remaining != 1 || result.notFound != 1 {
		t.Fatalf("counts checked=%d failed=%d remaining=%d notFound=%d", result.checked, result.failed, result.remaining, result.notFound)
	}

	// A second scan asks the catalog only about the book that failed.
	before := fake.calls.Load()
	suggestAudiobookSeries(context.Background(), lookup, books, existing, true)
	if extra := fake.calls.Load() - before; extra != 1 {
		t.Fatalf("second scan made %d catalog calls, want 1 (the failed book)", extra)
	}
}

func TestSuggestAudiobookSeriesSkipsAssignedByDefault(t *testing.T) {
	fake := &fakeSeriesLookup{results: map[string][]audiobookSeriesMatch{
		"Mistborn": {{Key: "mistborn", Name: "Mistborn", Sequence: floatPtr(1)}},
	}}
	books := []audiobookRecord{{ID: "mist", Title: "Mistborn", SeriesID: "s-other"}}
	result := suggestAudiobookSeries(context.Background(), newCachedSeriesLookup(fake), books, nil, false)
	if fake.calls.Load() != 0 || len(result.suggestions) != 0 || result.checked != 0 {
		t.Fatalf("assigned book was looked up: calls=%d result=%+v", fake.calls.Load(), result)
	}
}

// An existing series whose books are only partly filed is still suggested,
// pointing at that series rather than proposing a duplicate.
func TestSuggestAudiobookSeriesMatchesExistingSeriesByName(t *testing.T) {
	fake := &fakeSeriesLookup{results: map[string][]audiobookSeriesMatch{
		"Mistborn":              {{Key: "mistborn", Name: "Mistborn", Sequence: floatPtr(1)}},
		"The Well of Ascension": {{Key: "mistborn", Name: "Mistborn", Sequence: floatPtr(2)}},
	}}
	books := []audiobookRecord{
		{ID: "mist", Title: "Mistborn", SeriesID: "s-mist", SeriesSequence: floatPtr(1)},
		{ID: "well", Title: "The Well of Ascension"},
	}
	existing := []audiobookSeries{{ID: "s-mist", Name: "Mistborn"}}
	result := suggestAudiobookSeries(context.Background(), newCachedSeriesLookup(fake), books, existing, true)
	if len(result.suggestions) != 1 || result.suggestions[0].existingSeriesID != "s-mist" || len(result.suggestions[0].books) != 2 {
		t.Fatalf("suggestions = %+v", result.suggestions)
	}
}
