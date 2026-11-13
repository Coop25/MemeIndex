package manager

import (
	"errors"
	"slices"
	"strings"
	"time"

	"memeindex/internal/accessor"
)

// Recomputing the tag-hygiene report is a full O(n^2) pass over every distinct
// tag name, so it runs on a background worker rather than the admin request
// path. A burst of tag edits is coalesced into one recompute after
// tagHygieneDebounce of quiet; the worker also re-scans every
// tagHygieneMaxInterval as a backstop in case a dirty signal is ever missed.
// Both are var (not const) only so tests can shrink them.
var (
	tagHygieneDebounce    = 5 * time.Second
	tagHygieneMaxInterval = 10 * time.Minute
)

// Paging defaults for the admin tag-hygiene panel. The catalog list in
// particular can run to thousands of rows, so callers page through it.
const (
	tagHygienePageDefaultLimit = 50
	tagHygienePageMaxLimit     = 200
)

type TagHygieneReport struct {
	Tags  []TagHygieneTag  `json:"tags"`
	Pairs []TagHygienePair `json:"pairs"`
}

type TagHygieneTag struct {
	Tag     string   `json:"tag"`
	Count   int      `json:"count"`
	Similar []string `json:"similar,omitempty"`
}

type TagHygienePair struct {
	Primary   string `json:"primary"`
	Candidate string `json:"candidate"`
}

// TagHygienePageParams selects and pages a slice of the tag-hygiene report.
// View "" returns the first page of both lists (the legacy payload shape);
// "pairs" or "tags" page a single list and drive HasMore/NextOffset.
type TagHygienePageParams struct {
	View   string
	Offset int
	Limit  int
	Search string // case-insensitive tag substring filter
}

// TagHygienePageResult is a paged view of TagHygieneReport plus snapshot
// freshness metadata. Ready is false (with empty lists) until the background
// worker has produced its first snapshot.
type TagHygienePageResult struct {
	Tags       []TagHygieneTag  `json:"tags"`
	Pairs      []TagHygienePair `json:"pairs"`
	TagTotal   int              `json:"tag_total"`
	PairTotal  int              `json:"pair_total"`
	Offset     int              `json:"offset"`
	Limit      int              `json:"limit"`
	HasMore    bool             `json:"has_more"`
	NextOffset int              `json:"next_offset"`
	Ready      bool             `json:"ready"`
	ComputedAt time.Time        `json:"computed_at"`
}

type TagMergeResult struct {
	SourceTag     string `json:"source_tag"`
	TargetTag     string `json:"target_tag"`
	AffectedMemes int    `json:"affected_memes"`
	// AudiobooksAffected and RestrictionsMoved count audiobook tag links and
	// account audiobook-tag restrictions re-pointed onto the target tag.
	AudiobooksAffected int `json:"audiobooks_affected"`
	RestrictionsMoved  int `json:"restrictions_moved"`
}

// TagHygieneReport returns the full tag-hygiene read model. It is served from a
// snapshot kept warm by the background worker (see StartTagHygieneWorker); with
// no worker running it computes and memoizes the report inline on first use.
// Callers receive an independent deep copy.
func (m *MemeManager) TagHygieneReport() TagHygieneReport {
	report, _, _ := m.tagHygieneSnapshot()
	return report
}

// TagHygienePage returns a paged slice of the tag-hygiene report plus snapshot
// freshness metadata, without ever running the O(n^2) recompute on the caller's
// goroutine when the background worker is active.
func (m *MemeManager) TagHygienePage(params TagHygienePageParams) TagHygienePageResult {
	report, ready, computedAt := m.tagHygieneSnapshot()

	limit := params.Limit
	if limit <= 0 {
		limit = tagHygienePageDefaultLimit
	}
	if limit > tagHygienePageMaxLimit {
		limit = tagHygienePageMaxLimit
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	search := strings.ToLower(strings.TrimSpace(params.Search))

	out := TagHygienePageResult{
		Tags:       []TagHygieneTag{},
		Pairs:      []TagHygienePair{},
		Offset:     offset,
		Limit:      limit,
		Ready:      ready,
		ComputedAt: computedAt,
	}

	wantPairs := params.View == "" || params.View == "pairs"
	wantTags := params.View == "" || params.View == "tags"

	if wantPairs {
		matched := report.Pairs
		if search != "" {
			matched = make([]TagHygienePair, 0, len(report.Pairs))
			for _, pair := range report.Pairs {
				if strings.Contains(strings.ToLower(pair.Primary), search) ||
					strings.Contains(strings.ToLower(pair.Candidate), search) {
					matched = append(matched, pair)
				}
			}
		}
		out.PairTotal = len(matched)
		lo, hi := pageBounds(len(matched), offset, limit)
		out.Pairs = append(out.Pairs, matched[lo:hi]...)
		if params.View == "pairs" {
			out.HasMore = hi < len(matched)
			out.NextOffset = hi
		}
	}

	if wantTags {
		matched := report.Tags
		if search != "" {
			matched = make([]TagHygieneTag, 0, len(report.Tags))
			for _, tag := range report.Tags {
				if strings.Contains(strings.ToLower(tag.Tag), search) {
					matched = append(matched, tag)
				}
			}
		}
		out.TagTotal = len(matched)
		lo, hi := pageBounds(len(matched), offset, limit)
		out.Tags = append(out.Tags, matched[lo:hi]...)
		if params.View == "tags" {
			out.HasMore = hi < len(matched)
			out.NextOffset = hi
		}
	}

	return out
}

func pageBounds(n, offset, limit int) (int, int) {
	if offset >= n {
		return n, n
	}
	hi := offset + limit
	if hi > n {
		hi = n
	}
	return offset, hi
}

// tagHygieneSnapshot returns the current warm snapshot. With a running worker it
// never blocks: a nil snapshot means the first computation is still in flight,
// reported as Ready=false. With no worker it computes and memoizes inline.
func (m *MemeManager) tagHygieneSnapshot() (report TagHygieneReport, ready bool, computedAt time.Time) {
	m.adminReadMu.Lock()
	snap := m.tagHygieneCache
	at := m.tagHygieneCachedAt
	workerRunning := m.tagHygieneRefresh != nil
	m.adminReadMu.Unlock()

	if snap != nil {
		return cloneTagHygieneReport(*snap), true, at
	}
	if workerRunning {
		m.signalTagHygieneRefresh()
		return TagHygieneReport{Tags: []TagHygieneTag{}, Pairs: []TagHygienePair{}}, false, time.Time{}
	}

	fresh := m.computeTagHygieneReport()
	stored := cloneTagHygieneReport(fresh)
	m.adminReadMu.Lock()
	m.tagHygieneCache = &stored
	m.tagHygieneCachedAt = time.Now()
	m.tagHygieneDirty = false
	at = m.tagHygieneCachedAt
	m.adminReadMu.Unlock()
	return fresh, true, at
}

// StartTagHygieneWorker launches the background goroutine that keeps the
// tag-hygiene snapshot warm so the admin panel never waits on a full recompute.
// Safe to call repeatedly; only the first call starts the worker.
func (m *MemeManager) StartTagHygieneWorker() {
	m.tagHygieneWorkerStart.Do(func() {
		m.adminReadMu.Lock()
		m.tagHygieneRefresh = make(chan struct{}, 1)
		m.adminReadMu.Unlock()
		m.workerWG.Add(1)
		go func() {
			defer m.workerWG.Done()
			m.runTagHygieneWorker()
		}()
	})
}

func (m *MemeManager) runTagHygieneWorker() {
	m.adminReadMu.Lock()
	refresh := m.tagHygieneRefresh
	m.adminReadMu.Unlock()

	m.refreshTagHygieneSnapshot() // prime it so the first admin visit is instant

	ticker := time.NewTicker(tagHygieneMaxInterval)
	defer ticker.Stop()

	debounce := time.NewTimer(time.Hour)
	debounce.Stop()
	pending := false

	for {
		select {
		case <-m.workerCtx.Done():
			return
		case <-refresh:
			pending = true
			debounce.Reset(tagHygieneDebounce)
		case <-debounce.C:
			if pending {
				pending = false
				m.refreshTagHygieneSnapshot()
			}
		case <-ticker.C:
			m.adminReadMu.Lock()
			stale := m.tagHygieneDirty || time.Since(m.tagHygieneCachedAt) >= tagHygieneMaxInterval
			m.adminReadMu.Unlock()
			if stale {
				pending = false
				m.refreshTagHygieneSnapshot()
			}
		}
	}
}

func (m *MemeManager) refreshTagHygieneSnapshot() {
	m.adminReadMu.Lock()
	m.tagHygieneDirty = false
	m.adminReadMu.Unlock()

	report := m.computeTagHygieneReport()
	stored := cloneTagHygieneReport(report)

	m.adminReadMu.Lock()
	m.tagHygieneCache = &stored
	m.tagHygieneCachedAt = time.Now()
	m.adminReadMu.Unlock()
}

// signalTagHygieneRefresh wakes the background worker (no-op if it is not
// running). The refresh channel is buffered to one, so redundant pokes collapse.
func (m *MemeManager) signalTagHygieneRefresh() {
	m.adminReadMu.Lock()
	ch := m.tagHygieneRefresh
	m.adminReadMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// markTagsChanged records that the set of tags on some meme changed, so the
// tag-hygiene snapshot is stale. It only pokes the background worker (or, with
// no worker, drops the memoized snapshot for the next reader) - it never runs
// the recompute on the caller's goroutine.
func (m *MemeManager) markTagsChanged() {
	m.adminReadMu.Lock()
	m.tagHygieneDirty = true
	workerRunning := m.tagHygieneRefresh != nil
	if !workerRunning {
		m.tagHygieneCache = nil
	}
	m.adminReadMu.Unlock()
	if workerRunning {
		m.signalTagHygieneRefresh()
	}
}

func (m *MemeManager) computeTagHygieneReport() TagHygieneReport {
	tagCounts := m.tagUsageCounts()

	tagNames := make([]string, 0, len(tagCounts))
	for tag := range tagCounts {
		tagNames = append(tagNames, tag)
	}
	slices.Sort(tagNames)

	// Derive each tag's compact key once up front. This loop is O(n^2) over the
	// tag set, and compactTagKey used to be recomputed twice per pair - each call
	// building a fresh strings.Replacer - which is what made the panel crawl.
	compactKeys := make([]string, len(tagNames))
	for i, tag := range tagNames {
		compactKeys[i] = compactTagKey(tag)
	}

	similarMap := map[string][]string{}
	pairs := make([]TagHygienePair, 0)
	for index, left := range tagNames {
		leftKey := compactKeys[index]
		for offset, right := range tagNames[index+1:] {
			if !tagKeysLookLikeVariant(leftKey, compactKeys[index+1+offset]) {
				continue
			}
			primary := preferredCanonicalTag(left, right, tagCounts)
			candidate := right
			if primary == right {
				candidate = left
			}
			similarMap[primary] = append(similarMap[primary], candidate)
			pairs = append(pairs, TagHygienePair{
				Primary:   primary,
				Candidate: candidate,
			})
		}
	}

	items := make([]TagHygieneTag, 0, len(tagNames))
	for _, tag := range tagNames {
		items = append(items, TagHygieneTag{
			Tag:     tag,
			Count:   tagCounts[tag],
			Similar: normalizeTags(similarMap[tag]),
		})
	}
	slices.SortFunc(items, func(left, right TagHygieneTag) int {
		if len(left.Similar) != len(right.Similar) {
			return len(right.Similar) - len(left.Similar)
		}
		if left.Count != right.Count {
			return right.Count - left.Count
		}
		return strings.Compare(left.Tag, right.Tag)
	})

	slices.SortFunc(pairs, func(left, right TagHygienePair) int {
		if left.Primary != right.Primary {
			return strings.Compare(left.Primary, right.Primary)
		}
		return strings.Compare(left.Candidate, right.Candidate)
	})

	return TagHygieneReport{
		Tags:  items,
		Pairs: pairs,
	}
}

func cloneTagHygieneReport(in TagHygieneReport) TagHygieneReport {
	out := TagHygieneReport{
		Tags:  make([]TagHygieneTag, len(in.Tags)),
		Pairs: append([]TagHygienePair(nil), in.Pairs...),
	}
	for i, tag := range in.Tags {
		tag.Similar = append([]string(nil), in.Tags[i].Similar...)
		out.Tags[i] = tag
	}
	return out
}

func (m *MemeManager) MergeTags(sourceTag string, targetTag string, actor accessor.AuditActor) (TagMergeResult, error) {
	sourceTag = strings.ToLower(strings.TrimSpace(sourceTag))
	targetTag = strings.ToLower(strings.TrimSpace(targetTag))
	if sourceTag == "" || targetTag == "" {
		return TagMergeResult{}, errors.New("source and target tags are required")
	}
	if sourceTag == targetTag {
		return TagMergeResult{
			SourceTag: sourceTag,
			TargetTag: targetTag,
		}, nil
	}

	// Fast path: one transaction that repoints tag links in bulk, instead of
	// rewriting every affected meme (each of which re-swept the tags table).
	if tm, ok := m.store.(accessor.TagMaintenanceStore); ok {
		counts, err := tm.MergeTag(sourceTag, targetTag, actor)
		if err != nil {
			return TagMergeResult{}, err
		}
		if counts.Memes > 0 {
			m.invalidateAdminReadCache()
		}
		return TagMergeResult{
			SourceTag:          sourceTag,
			TargetTag:          targetTag,
			AffectedMemes:      counts.Memes,
			AudiobooksAffected: counts.Audiobooks,
			RestrictionsMoved:  counts.Restrictions,
		}, nil
	}

	memes := m.store.List("", "", false, "")
	result := TagMergeResult{
		SourceTag: sourceTag,
		TargetTag: targetTag,
	}
	for _, meme := range memes {
		if !containsTag(meme.Tags, sourceTag) {
			continue
		}
		nextTags := make([]string, 0, len(meme.Tags))
		for _, tag := range meme.Tags {
			normalized := strings.ToLower(strings.TrimSpace(tag))
			if normalized == sourceTag {
				nextTags = append(nextTags, targetTag)
				continue
			}
			nextTags = append(nextTags, normalized)
		}
		if _, err := m.store.Update("", meme.ID, accessor.MemeUpdate{
			Tags:     nextTags,
			Notes:    meme.Notes,
			Favorite: meme.Favorite,
			Actor:    actor,
		}); err != nil {
			return TagMergeResult{}, err
		}
		result.AffectedMemes += 1
	}

	if result.AffectedMemes > 0 {
		m.invalidateAdminReadCache()
	}
	return result, nil
}

// tagKeysLookLikeVariant reports whether two tags look like variants of one
// another, given compact keys the caller already derived (see compactTagKey).
// computeTagHygieneReport
// precomputes one key per tag and calls this for every candidate pair, so the
// per-pair work stays down to a length check and an occasional edit-distance.
func tagKeysLookLikeVariant(leftKey string, rightKey string) bool {
	if leftKey == "" || rightKey == "" {
		return false
	}
	if leftKey == rightKey {
		return true
	}
	if absInt(len(leftKey)-len(rightKey)) > 2 {
		return false
	}
	if min(len(leftKey), len(rightKey)) < 4 {
		return false
	}
	return levenshteinDistance(leftKey, rightKey) <= 2
}

func preferredCanonicalTag(left string, right string, counts map[string]int) string {
	left = strings.ToLower(strings.TrimSpace(left))
	right = strings.ToLower(strings.TrimSpace(right))
	if counts[left] != counts[right] {
		if counts[left] > counts[right] {
			return left
		}
		return right
	}
	if compactTagKey(left) == compactTagKey(right) {
		if len(left) < len(right) {
			return left
		}
		if len(right) < len(left) {
			return right
		}
	}
	if strings.Compare(left, right) <= 0 {
		return left
	}
	return right
}

// tagKeyReplacer strips the separators and quote marks that make two tags read
// as different while meaning the same thing. Hoisted to package scope: it was
// being rebuilt on every compactTagKey call, which the O(n^2) hygiene pair-scan
// hit millions of times per report.
var tagKeyReplacer = strings.NewReplacer(
	" ", "", "-", "", "_", "", ".", "", ",", "", "'", "", "\"", "",
)

func compactTagKey(tag string) string {
	return tagKeyReplacer.Replace(strings.ToLower(strings.TrimSpace(tag)))
}

func levenshteinDistance(left string, right string) int {
	if left == right {
		return 0
	}
	if left == "" {
		return len(right)
	}
	if right == "" {
		return len(left)
	}

	prev := make([]int, len(right)+1)
	curr := make([]int, len(right)+1)
	for column := 0; column <= len(right); column++ {
		prev[column] = column
	}

	for row := 1; row <= len(left); row++ {
		curr[0] = row
		for column := 1; column <= len(right); column++ {
			cost := 0
			if left[row-1] != right[column-1] {
				cost = 1
			}
			curr[column] = minInt(
				curr[column-1]+1,
				prev[column]+1,
				prev[column-1]+cost,
			)
		}
		copy(prev, curr)
	}

	return prev[len(right)]
}

func minInt(values ...int) int {
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}
	return best
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
