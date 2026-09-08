package accessor

import (
	"strings"
	"testing"
	"time"
)

func TestParseMemeSearchBareTerms(t *testing.T) {
	got := ParseMemeSearch("  Funny   CAT ")
	if want := []string{"funny", "cat"}; !equalStrings(got.Terms, want) {
		t.Fatalf("Terms = %v, want %v", got.Terms, want)
	}
	if !got.After.IsZero() || !got.Before.IsZero() || len(got.Tags) != 0 || len(got.Types) != 0 || len(got.Negations) != 0 {
		t.Fatalf("unexpected non-term content: %+v", got)
	}
}

func TestParseMemeSearchQuotedPhrase(t *testing.T) {
	got := ParseMemeSearch(`"happy birthday" cat`)
	if want := []string{"happy birthday", "cat"}; !equalStrings(got.Terms, want) {
		t.Fatalf("Terms = %v, want %v", got.Terms, want)
	}
}

func TestParseMemeSearchNegation(t *testing.T) {
	got := ParseMemeSearch("meme -blurry -\"low res\"")
	if want := []string{"meme"}; !equalStrings(got.Terms, want) {
		t.Fatalf("Terms = %v, want %v", got.Terms, want)
	}
	if want := []string{"blurry", "low res"}; !equalStrings(got.Negations, want) {
		t.Fatalf("Negations = %v, want %v", got.Negations, want)
	}
}

func TestParseMemeSearchOperators(t *testing.T) {
	got := ParseMemeSearch(`TAG:Reaction tag:"two words" type:videos type:img`)
	if want := []string{"reaction", "two words"}; !equalStrings(got.Tags, want) {
		t.Fatalf("Tags = %v, want %v", got.Tags, want)
	}
	if want := []string{"video", "image"}; !equalStrings(got.Types, want) {
		t.Fatalf("Types = %v, want %v", got.Types, want)
	}
}

func TestParseMemeSearchDates(t *testing.T) {
	got := ParseMemeSearch("after:2024-01-31 before:2024/03/01")
	if !got.After.Equal(time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("After = %v", got.After)
	}
	if !got.Before.Equal(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Before = %v", got.Before)
	}
}

func TestParseMemeSearchDropsNoise(t *testing.T) {
	for _, raw := range []string{"", "   ", "type:bogus", "after:notadate", `""`} {
		if got := ParseMemeSearch(raw); !got.IsZero() {
			t.Fatalf("ParseMemeSearch(%q) = %+v, want zero", raw, got)
		}
	}
}

func TestParseMemeSearchDeduplicates(t *testing.T) {
	got := ParseMemeSearch("cat cat CAT -dog -dog")
	if want := []string{"cat"}; !equalStrings(got.Terms, want) {
		t.Fatalf("Terms = %v, want %v", got.Terms, want)
	}
	if want := []string{"dog"}; !equalStrings(got.Negations, want) {
		t.Fatalf("Negations = %v, want %v", got.Negations, want)
	}
}

func TestBuildMemeWhereEmpty(t *testing.T) {
	where, args, next := buildMemeWhere(MemeSearch{}, "", 2)
	if where != "COALESCE(m.hidden_from_app, FALSE) = FALSE" {
		t.Fatalf("where = %q", where)
	}
	if len(args) != 0 || next != 2 {
		t.Fatalf("args = %v, next = %d", args, next)
	}
}

func TestBuildMemeWhereTermTagAndNegation(t *testing.T) {
	search := MemeSearch{Terms: []string{"cat"}, Negations: []string{"blur"}, Tags: []string{"reaction"}}
	where, args, next := buildMemeWhere(search, "spicy", 2)

	if want := []any{"%cat%", "%blur%", "%reaction%", "%spicy%"}; !equalAny(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	if next != 6 {
		t.Fatalf("next = %d, want 6", next)
	}
	for _, frag := range []string{
		"LOWER(m.original_name) LIKE $2",
		"LOWER(COALESCE(m.search_text, '')) LIKE $2",
		"NOT (LOWER(m.original_name) LIKE $3",
		"tx.name LIKE $4",
		"tx.name LIKE $5",
	} {
		if !strings.Contains(where, frag) {
			t.Fatalf("where missing %q:\n%s", frag, where)
		}
	}
	// Subquery aliases must not collide with the outer LEFT JOIN aliases.
	if strings.Contains(where, "FROM meme_tags mt ") || strings.Contains(where, "JOIN tags t ") {
		t.Fatalf("where reuses outer join aliases:\n%s", where)
	}
}

func TestBuildMemeWhereTypesAndDates(t *testing.T) {
	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	where, args, next := buildMemeWhere(MemeSearch{Types: []string{"image", "video"}, After: after, Before: before}, "", 2)

	if !strings.Contains(where, "m.content_type LIKE 'image/%'") || !strings.Contains(where, "m.content_type LIKE 'video/%'") {
		t.Fatalf("where missing type conditions:\n%s", where)
	}
	if !strings.Contains(where, "m.created_at >= $2") || !strings.Contains(where, "m.created_at < $3") {
		t.Fatalf("where missing date bounds:\n%s", where)
	}
	if len(args) != 2 || next != 4 {
		t.Fatalf("args = %v, next = %d", args, next)
	}
}

func TestMemeSortOrder(t *testing.T) {
	cases := map[string]string{
		"":        "m.created_at DESC, m.id ASC",
		"newest":  "m.created_at DESC, m.id ASC",
		"oldest":  "m.created_at ASC, m.id ASC",
		"name":    "LOWER(m.original_name) ASC, m.id ASC",
		"size":    "m.size_bytes DESC, m.id ASC",
		"updated": "m.updated_at DESC, m.id ASC",
		"garbage": "m.created_at DESC, m.id ASC",
	}
	for in, want := range cases {
		if got := memeSortOrder(in); got != want {
			t.Fatalf("memeSortOrder(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMemeViewCondition(t *testing.T) {
	if got := memeViewCondition("favorites", 1); !strings.Contains(got, "uf.user_id = $1") {
		t.Fatalf("favorites view = %q", got)
	}
	if got := memeViewCondition("untagged", 1); !strings.HasPrefix(got, "NOT EXISTS") {
		t.Fatalf("untagged view = %q", got)
	}
	if got := memeViewCondition("", 1); got != "" {
		t.Fatalf("empty view = %q, want \"\"", got)
	}
	if got := memeViewCondition("nonsense", 1); got != "" {
		t.Fatalf("unknown view = %q, want \"\"", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalAny(a []any, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
