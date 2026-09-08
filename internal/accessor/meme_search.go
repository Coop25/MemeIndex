package accessor

import (
	"strconv"
	"strings"
	"time"
)

// MemeSearch is a parsed meme search string.
//
// Bare words become AND-matched, case-insensitive substrings against a meme's
// searchable text (original name, notes, source URL, content type, or any tag
// name). Double quotes group a phrase. A leading "-" negates a word. A handful
// of "field:value" operators narrow further:
//
//	tag:reaction      require a tag whose name contains "reaction"
//	type:video        restrict to a media class (image|video|audio|file)
//	after:2024-01-31  created on/after a date (YYYY-MM-DD or YYYY/MM/DD)
//	before:2024-03-01 created strictly before a date
//
// An empty or all-noise string parses to the zero value, which matches
// everything (subject to the caller's other filters).
type MemeSearch struct {
	Terms     []string
	Negations []string
	Tags      []string
	Types     []string
	After     time.Time
	Before    time.Time
}

// IsZero reports whether the search imposes no constraint of its own.
func (s MemeSearch) IsZero() bool {
	return len(s.Terms) == 0 && len(s.Negations) == 0 && len(s.Tags) == 0 &&
		len(s.Types) == 0 && s.After.IsZero() && s.Before.IsZero()
}

// ParseMemeSearch turns a raw search box string into a MemeSearch.
func ParseMemeSearch(raw string) MemeSearch {
	var s MemeSearch
	seen := map[string]struct{}{}
	add := func(key string) bool {
		if _, ok := seen[key]; ok {
			return false
		}
		seen[key] = struct{}{}
		return true
	}

	for _, tok := range tokenizeSearch(raw) {
		negated := false
		if len(tok) > 1 && tok[0] == '-' {
			negated = true
			tok = tok[1:]
		}
		lower := strings.ToLower(tok)

		switch {
		case strings.HasPrefix(lower, "tag:"):
			v := cleanSearchValue(tok[len("tag:"):])
			if v != "" && add("tag:"+v) {
				s.Tags = append(s.Tags, v)
			}
		case strings.HasPrefix(lower, "type:"):
			if v := normalizeSearchType(tok[len("type:"):]); v != "" && add("type:"+v) {
				s.Types = append(s.Types, v)
			}
		case strings.HasPrefix(lower, "after:"):
			if ts, ok := parseSearchDate(tok[len("after:"):]); ok {
				s.After = ts
			}
		case strings.HasPrefix(lower, "before:"):
			if ts, ok := parseSearchDate(tok[len("before:"):]); ok {
				s.Before = ts
			}
		default:
			v := cleanSearchValue(tok)
			if v == "" {
				continue
			}
			if negated {
				if add("neg:" + v) {
					s.Negations = append(s.Negations, v)
				}
				continue
			}
			if add("term:" + v) {
				s.Terms = append(s.Terms, v)
			}
		}
	}
	return s
}

// tokenizeSearch splits on unquoted whitespace, keeping double-quoted groups
// (including quotes attached to an operator, e.g. tag:"two words") intact. The
// surrounding quote characters are left on the token and stripped later.
func tokenizeSearch(raw string) []string {
	var toks []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range raw {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case !inQuote && (r == ' ' || r == '\t' || r == '\n' || r == '\r'):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return toks
}

func cleanSearchValue(v string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(v, `"`, "")))
}

func normalizeSearchType(v string) string {
	switch cleanSearchValue(v) {
	case "image", "images", "img", "pic", "picture", "photo":
		return "image"
	case "video", "videos", "vid":
		return "video"
	case "audio", "sound", "mp3", "mp3s":
		return "audio"
	case "file", "files", "doc", "document", "other":
		return "file"
	}
	return ""
}

func parseSearchDate(v string) (time.Time, bool) {
	v = cleanSearchValue(v)
	for _, layout := range []string{"2006-01-02", "2006/01/02"} {
		if ts, err := time.Parse(layout, v); err == nil {
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}

// --- SQL fragment builders (kept pure so they can be unit-tested without a DB) ---
//
// All user-supplied values are emitted as positional placeholders ($N); only
// fixed, code-chosen SQL structure is ever concatenated.

// memeSearchTextColumns are the columns a bare term is matched against, in
// addition to the meme's tag names. It is fixed SQL structure, never mutated, so
// it is a package-level slice rather than a fresh allocation per search term.
var memeSearchTextColumns = []string{
	"LOWER(m.original_name)",
	"LOWER(m.notes)",
	"LOWER(COALESCE(m.source_url, ''))",
	"LOWER(m.content_type)",
	"LOWER(COALESCE(m.search_text, ''))",
}

// buildMemeWhere renders the WHERE body shared by the counts query and the page
// query: the hidden-from-app guard, the parsed search, and an explicit tag
// filter. Callers reserve $1 for the favourites user id and pass startArg=2, so
// view/favorites clauses appended afterwards can reference $1. Args are
// positional starting at startArg; nextArg is the first unused placeholder.
func buildMemeWhere(search MemeSearch, tag string, startArg int) (where string, args []any, nextArg int) {
	conds := []string{"COALESCE(m.hidden_from_app, FALSE) = FALSE"}
	arg := startArg

	textMatch := func(value string) string {
		ph := "$" + strconv.Itoa(arg)
		args = append(args, "%"+value+"%")
		arg++
		parts := make([]string, 0, len(memeSearchTextColumns)+1)
		for _, col := range memeSearchTextColumns {
			parts = append(parts, col+" LIKE "+ph)
		}
		parts = append(parts, "EXISTS (SELECT 1 FROM meme_tags mtx JOIN tags tx ON tx.id = mtx.tag_id WHERE mtx.meme_id = m.id AND tx.name LIKE "+ph+")")
		return "(" + strings.Join(parts, " OR ") + ")"
	}
	tagMatch := func(value string) string {
		ph := "$" + strconv.Itoa(arg)
		args = append(args, "%"+value+"%")
		arg++
		return "EXISTS (SELECT 1 FROM meme_tags mtx JOIN tags tx ON tx.id = mtx.tag_id WHERE mtx.meme_id = m.id AND tx.name LIKE " + ph + ")"
	}

	for _, term := range search.Terms {
		conds = append(conds, textMatch(term))
	}
	for _, neg := range search.Negations {
		conds = append(conds, "NOT "+textMatch(neg))
	}
	for _, t := range search.Tags {
		conds = append(conds, tagMatch(t))
	}
	if len(search.Types) > 0 {
		typeConds := make([]string, 0, len(search.Types))
		for _, ty := range search.Types {
			if c := memeTypeCondition(ty); c != "" {
				typeConds = append(typeConds, c)
			}
		}
		if len(typeConds) > 0 {
			conds = append(conds, "("+strings.Join(typeConds, " OR ")+")")
		}
	}
	if !search.After.IsZero() {
		conds = append(conds, "m.created_at >= $"+strconv.Itoa(arg))
		args = append(args, search.After.UTC())
		arg++
	}
	if !search.Before.IsZero() {
		conds = append(conds, "m.created_at < $"+strconv.Itoa(arg))
		args = append(args, search.Before.UTC())
		arg++
	}
	if strings.TrimSpace(tag) != "" {
		conds = append(conds, tagMatch(normalizeTag(tag)))
	}

	return strings.Join(conds, "\n  AND "), args, arg
}

// memeTypeCondition mirrors the media-class buckets used by the in-memory view
// filter and facet counts.
func memeTypeCondition(t string) string {
	switch t {
	case "image":
		return "m.content_type LIKE 'image/%'"
	case "video":
		return "m.content_type LIKE 'video/%'"
	case "audio":
		return "(m.content_type LIKE 'audio/%' OR LOWER(m.original_name) LIKE '%.mp3')"
	case "file":
		return "(m.content_type NOT LIKE 'image/%' AND m.content_type NOT LIKE 'video/%' AND m.content_type NOT LIKE 'audio/%' AND LOWER(m.original_name) NOT LIKE '%.mp3')"
	}
	return ""
}

// memeViewCondition translates a grid "view" into a WHERE fragment. favUserArg
// is the placeholder holding the favourites user id. An unknown/empty view
// returns "" (no extra restriction).
func memeViewCondition(view string, favUserArg int) string {
	switch strings.ToLower(strings.TrimSpace(view)) {
	case "favorites":
		return "EXISTS (SELECT 1 FROM user_favorites uf WHERE uf.user_id = $" + strconv.Itoa(favUserArg) + " AND uf.meme_id = m.id)"
	case "videos":
		return "m.content_type LIKE 'video/%'"
	case "images":
		return "m.content_type LIKE 'image/%'"
	case "mp3s":
		return "(m.content_type LIKE 'audio/%' OR LOWER(m.original_name) LIKE '%.mp3')"
	case "untagged":
		return "NOT EXISTS (SELECT 1 FROM meme_tags mtv WHERE mtv.meme_id = m.id)"
	case "files":
		return "(m.content_type NOT LIKE 'image/%' AND m.content_type NOT LIKE 'video/%' AND m.content_type NOT LIKE 'audio/%' AND LOWER(m.original_name) NOT LIKE '%.mp3')"
	}
	return ""
}

// memeSortOrder maps a sort key to an ORDER BY body. Every ordering ends with a
// stable "m.id" tiebreaker so pagination cannot drop or repeat rows.
func memeSortOrder(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "oldest":
		return "m.created_at ASC, m.id ASC"
	case "name":
		return "LOWER(m.original_name) ASC, m.id ASC"
	case "size":
		return "m.size_bytes DESC, m.id ASC"
	case "updated":
		return "m.updated_at DESC, m.id ASC"
	default:
		return "m.created_at DESC, m.id ASC"
	}
}
