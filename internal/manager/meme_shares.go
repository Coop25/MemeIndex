package manager

import (
	"errors"
	"log"
	"strings"
	"time"

	"memeindex/internal/accessor"
)

func (m *MemeManager) GetOrCreateMemeShare(memeID, userID string, now time.Time) (accessor.MemeShareState, error) {
	store, ok := m.store.(accessor.MemeShareStore)
	if !ok {
		return accessor.MemeShareState{}, errors.New("meme sharing is unavailable")
	}
	return store.GetOrCreateMemeShare(strings.TrimSpace(memeID), strings.TrimSpace(userID), now.UTC(), now.UTC().Add(MemeShareLifetime))
}

// MemeShareLifetime is how long a share link serves its file. Sharing a meme
// that is already shared renews the same link for this long again.
const MemeShareLifetime = 365 * 24 * time.Hour

// ResolveMemeShareLink looks up a share link code. current reports whether the
// code belongs to the meme's current share generation; an older code still
// resolves so its link can redirect to the meme, but must not serve the file.
func (m *MemeManager) ResolveMemeShareLink(slug string) (share accessor.MemeShareState, meme accessor.Meme, current bool, err error) {
	store, ok := m.store.(accessor.MemeShareStore)
	if !ok {
		return accessor.MemeShareState{}, accessor.Meme{}, false, errors.New("meme sharing is unavailable")
	}
	memeID, generation, err := store.ResolveMemeShareSlug(strings.TrimSpace(slug))
	if err != nil {
		return accessor.MemeShareState{}, accessor.Meme{}, false, err
	}
	share, meme, err = m.GetMemeShare(memeID)
	if err != nil {
		return accessor.MemeShareState{}, accessor.Meme{}, false, err
	}
	return share, meme, generation == share.Generation, nil
}

func (m *MemeManager) GetMemeShare(memeID string) (accessor.MemeShareState, accessor.Meme, error) {
	store, ok := m.store.(accessor.MemeShareStore)
	if !ok {
		return accessor.MemeShareState{}, accessor.Meme{}, errors.New("meme sharing is unavailable")
	}
	share, err := store.GetMemeShareState(strings.TrimSpace(memeID))
	if err != nil {
		return accessor.MemeShareState{}, accessor.Meme{}, err
	}
	meme, err := m.store.GetByID("", share.MemeID)
	return share, meme, err
}

type ActiveMemeShare struct {
	Meme  accessor.Meme           `json:"meme"`
	Share accessor.MemeShareState `json:"share"`
}

func (m *MemeManager) ListActiveMemeShares(now time.Time) ([]ActiveMemeShare, error) {
	store, ok := m.store.(accessor.MemeShareStore)
	if !ok {
		return nil, errors.New("meme sharing is unavailable")
	}
	states, err := store.ListActiveMemeShares(now.UTC())
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(states))
	for _, state := range states {
		ids = append(ids, state.MemeID)
	}
	memes := m.memesByIDs(ids)

	result := make([]ActiveMemeShare, 0, len(states))
	for _, state := range states {
		meme, ok := memes[state.MemeID]
		if !ok {
			continue
		}
		result = append(result, ActiveMemeShare{Meme: meme, Share: state})
	}
	return result, nil
}

// Paging bounds for the admin shared-memes list.
const (
	activeSharesPageDefaultLimit = 25
	activeSharesPageMaxLimit     = 100
)

// ListActiveMemeSharesPage returns one page of active shares, newest first,
// with the total number of active shares. Only the page's memes are loaded.
// An offset past the end is pulled back to the last page, so revoking the only
// row on the final page does not leave the caller on an empty one.
func (m *MemeManager) ListActiveMemeSharesPage(now time.Time, offset, limit int) (shares []ActiveMemeShare, total int, pageOffset int, pageLimit int, err error) {
	store, ok := m.store.(accessor.MemeShareStore)
	if !ok {
		return nil, 0, 0, 0, errors.New("meme sharing is unavailable")
	}
	if limit <= 0 {
		limit = activeSharesPageDefaultLimit
	}
	if limit > activeSharesPageMaxLimit {
		limit = activeSharesPageMaxLimit
	}
	states, err := store.ListActiveMemeShares(now.UTC())
	if err != nil {
		return nil, 0, 0, 0, err
	}
	total = len(states)
	if offset < 0 {
		offset = 0
	}
	if total > 0 && offset >= total {
		offset = ((total - 1) / limit) * limit
	}
	if total == 0 {
		offset = 0
	}
	page := states[offset:min(offset+limit, total)]

	ids := make([]string, 0, len(page))
	for _, state := range page {
		ids = append(ids, state.MemeID)
	}
	memes := m.memesByIDs(ids)
	shares = make([]ActiveMemeShare, 0, len(page))
	for _, state := range page {
		if meme, ok := memes[state.MemeID]; ok {
			shares = append(shares, ActiveMemeShare{Meme: meme, Share: state})
		}
	}
	return shares, total, offset, limit, nil
}

// memesByIDs resolves visible memes by id in one batch where the store supports
// it (BulkMemeStore), falling back to per-id lookups otherwise. Missing ids are
// simply absent from the map.
func (m *MemeManager) memesByIDs(ids []string) map[string]accessor.Meme {
	if bulk, ok := m.store.(accessor.BulkMemeStore); ok {
		memes, err := bulk.MemesByIDs(ids)
		if err == nil {
			return memes
		}
		log.Printf("memes by ids: SQL batch failed, falling back to per-id reads: %v", err)
	}
	out := make(map[string]accessor.Meme, len(ids))
	for _, id := range ids {
		if meme, err := m.store.GetByID("", strings.TrimSpace(id)); err == nil {
			out[meme.ID] = meme
		}
	}
	return out
}

// memeNamesByIDs is memesByIDs reduced to just the original display name per id.
func (m *MemeManager) memeNamesByIDs(ids []string) map[string]string {
	memes := m.memesByIDs(ids)
	names := make(map[string]string, len(memes))
	for id, meme := range memes {
		names[id] = meme.OriginalName
	}
	return names
}

func (m *MemeManager) RevokeMemeShare(memeID string) error {
	store, ok := m.store.(accessor.MemeShareStore)
	if !ok {
		return errors.New("meme sharing is unavailable")
	}
	return store.RevokeMemeShare(strings.TrimSpace(memeID))
}

func (m *MemeManager) RevokeAllMemeShares(now time.Time) (int, error) {
	store, ok := m.store.(accessor.MemeShareStore)
	if !ok {
		return 0, errors.New("meme sharing is unavailable")
	}
	return store.RevokeAllMemeShares(now.UTC())
}
