// Package accessortest provides an in-memory accessor.Store for tests in other
// packages that need a working store without a database.
//
// It is test support only: production code must not import it. The server
// runs on accessor.PostgresStore.
package accessortest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"net/textproto"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"memeindex/internal/accessor"
)

var (
	_ accessor.Store                  = (*MemeStore)(nil)
	_ accessor.AdminMemeStore         = (*MemeStore)(nil)
	_ accessor.SuggestedTagStore      = (*MemeStore)(nil)
	_ accessor.FavoriteAnalyticsStore = (*MemeStore)(nil)
	_ accessor.MemeShareStore         = (*MemeStore)(nil)
	_ accessor.PreviewAssetStore      = (*MemeStore)(nil)
)

// localFavoritesUserID is the favorites bucket for an empty user id, matching
// the one PostgresStore uses.
const localFavoritesUserID = "__local__"

// MemeStore keeps meme metadata in memory and writes uploads to
// <dataDir>/uploads, so handlers that serve or inspect files on disk work as
// they do against PostgresStore. It has no visibility states (nothing is ever
// hidden), no audit history and no search index, and it never generates
// thumbnails: a meme's preview path reflects whatever thumbnail a test puts
// in <dataDir>/thumbnails.
type MemeStore struct {
	mu              sync.RWMutex
	uploadDir       string
	previewDir      string
	memes           []accessor.Meme
	byID            map[string]accessor.Meme
	byHash          map[string]accessor.Meme
	tagIndex        map[string]map[string]struct{}
	favoritesByUser map[string]map[string]struct{}
}

// NewMemeStore returns an empty store whose uploads live under dataDir.
func NewMemeStore(dataDir string) (*MemeStore, error) {
	uploadDir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	return &MemeStore{
		uploadDir:       uploadDir,
		previewDir:      filepath.Join(dataDir, "thumbnails"),
		memes:           []accessor.Meme{},
		byID:            map[string]accessor.Meme{},
		byHash:          map[string]accessor.Meme{},
		tagIndex:        map[string]map[string]struct{}{},
		favoritesByUser: map[string]map[string]struct{}{},
	}, nil
}

func (s *MemeStore) GetOrCreateMemeShare(memeID, userID string, now, expiresAt time.Time) (accessor.MemeShareState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	memeID = strings.TrimSpace(memeID)
	for i := range s.memes {
		if s.memes[i].ID != memeID {
			continue
		}
		if !s.memes[i].ShareExpiresAt.After(now) {
			s.memes[i].ShareGeneration++
			s.memes[i].SharedAt = now.UTC()
			s.memes[i].SharedByUserID = strings.TrimSpace(userID)
		}
		// An active share is renewed in place: same generation, same link.
		s.memes[i].ShareExpiresAt = expiresAt.UTC()
		if s.memes[i].ShareSlugs[s.memes[i].ShareGeneration] == "" {
			// Copy on write: Meme values handed out earlier share the old map.
			slugs := make(map[int64]string, len(s.memes[i].ShareSlugs)+1)
			for generation, slug := range s.memes[i].ShareSlugs {
				slugs[generation] = slug
			}
			slugs[s.memes[i].ShareGeneration] = uuid.NewString()
			s.memes[i].ShareSlugs = slugs
		}
		s.byID[memeID] = s.memes[i]
		return memeShareStateFromMeme(s.memes[i]), nil
	}
	return accessor.MemeShareState{}, os.ErrNotExist
}

func (s *MemeStore) GetMemeShareState(memeID string) (accessor.MemeShareState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	meme, ok := s.byID[strings.TrimSpace(memeID)]
	if !ok {
		return accessor.MemeShareState{}, os.ErrNotExist
	}
	return memeShareStateFromMeme(meme), nil
}

func (s *MemeStore) ResolveMemeShareSlug(slug string) (string, int64, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, os.ErrNotExist
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, meme := range s.memes {
		for generation, candidate := range meme.ShareSlugs {
			if candidate == slug {
				return meme.ID, generation, nil
			}
		}
	}
	return "", 0, os.ErrNotExist
}

func (s *MemeStore) ListActiveMemeShares(now time.Time) ([]accessor.MemeShareState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	shares := []accessor.MemeShareState{}
	for _, meme := range s.memes {
		if meme.ShareExpiresAt.After(now) {
			shares = append(shares, memeShareStateFromMeme(meme))
		}
	}
	sort.Slice(shares, func(i, j int) bool { return shares[i].SharedAt.After(shares[j].SharedAt) })
	return shares, nil
}

func (s *MemeStore) RevokeMemeShare(memeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	memeID = strings.TrimSpace(memeID)
	for i := range s.memes {
		if s.memes[i].ID != memeID {
			continue
		}
		s.memes[i].ShareGeneration++
		s.memes[i].ShareExpiresAt = time.Time{}
		s.byID[memeID] = s.memes[i]
		return nil
	}
	return os.ErrNotExist
}

func (s *MemeStore) RevokeAllMemeShares(now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revoked := 0
	for i := range s.memes {
		if !s.memes[i].ShareExpiresAt.After(now.UTC()) {
			continue
		}
		s.memes[i].ShareGeneration++
		s.memes[i].ShareExpiresAt = time.Time{}
		s.byID[s.memes[i].ID] = s.memes[i]
		revoked++
	}
	return revoked, nil
}

func memeShareStateFromMeme(meme accessor.Meme) accessor.MemeShareState {
	return accessor.MemeShareState{MemeID: meme.ID, Generation: meme.ShareGeneration, SharedByUserID: meme.SharedByUserID, SharedAt: meme.SharedAt, ExpiresAt: meme.ShareExpiresAt, Slug: meme.ShareSlugs[meme.ShareGeneration]}
}

func (s *MemeStore) List(userID, query string, favoritesOnly bool, tag string) []accessor.Meme {
	s.mu.RLock()
	defer s.mu.RUnlock()

	needle := strings.ToLower(strings.TrimSpace(query))
	tag = normalizeTag(tag)
	favorites := s.favoriteSetLocked(userID)
	candidates := s.memes
	if tag != "" {
		candidates = s.memesMatchingTagLocked(tag)
	}

	out := make([]accessor.Meme, 0, len(candidates))
	for _, meme := range candidates {
		isFavorite := isFavorite(favorites, meme.ID)
		if favoritesOnly && !isFavorite {
			continue
		}
		if needle != "" && !matchesQuery(meme, needle) {
			continue
		}

		meme.Favorite = isFavorite
		s.decoratePreviewPath(&meme)
		out = append(out, meme)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})

	return out
}

func (s *MemeStore) SuggestTags(prefix string, limit int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix = normalizeTag(prefix)
	if limit <= 0 {
		limit = 8
	}

	type rankedTag struct {
		name  string
		count int
	}
	ranked := make([]rankedTag, 0, len(s.tagIndex))
	for tag, memeIDs := range s.tagIndex {
		if prefix != "" && !strings.Contains(tag, prefix) {
			continue
		}
		ranked = append(ranked, rankedTag{name: tag, count: len(memeIDs)})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].name < ranked[j].name
	})
	out := make([]string, 0, min(limit, len(ranked)))
	for _, item := range ranked {
		out = append(out, item.name)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *MemeStore) GetByID(userID, id string) (accessor.Meme, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meme, ok := s.byID[strings.TrimSpace(id)]
	if !ok {
		return accessor.Meme{}, os.ErrNotExist
	}
	meme.Favorite = isFavorite(s.favoriteSetLocked(userID), meme.ID)
	s.decoratePreviewPath(&meme)
	return meme, nil
}

// GetAnyByID returns a meme without a viewer's favorite flag.
func (s *MemeStore) GetAnyByID(id string) (accessor.Meme, error) {
	return s.GetByID("", id)
}

// MemesByIDs returns the memes among ids that exist, keyed by id.
func (s *MemeStore) MemesByIDs(ids []string) (map[string]accessor.Meme, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]accessor.Meme, len(ids))
	for _, id := range ids {
		if meme, ok := s.byID[strings.TrimSpace(id)]; ok {
			s.decoratePreviewPath(&meme)
			out[meme.ID] = meme
		}
	}
	return out, nil
}

// AdminDashboardMemes returns every meme, newest first.
func (s *MemeStore) AdminDashboardMemes() ([]accessor.Meme, error) {
	s.mu.RLock()
	out := slices.Clone(s.memes)
	s.mu.RUnlock()
	for i := range out {
		s.decoratePreviewPath(&out[i])
	}
	slices.SortStableFunc(out, func(a, b accessor.Meme) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}

// HiddenMemeIDs is always empty: MemeStore cannot hide a meme.
func (s *MemeStore) HiddenMemeIDs() ([]string, error) {
	return nil, nil
}

// SetSearchText checks the meme exists and otherwise does nothing.
func (s *MemeStore) SetSearchText(id, _ string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.byID[strings.TrimSpace(id)]; !ok {
		return os.ErrNotExist
	}
	return nil
}

func (s *MemeStore) ListSuggestedTags(id string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meme, ok := s.byID[strings.TrimSpace(id)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]string(nil), meme.SuggestedTags...), nil
}

func (s *MemeStore) ReplaceSuggestedTags(id string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	for i := range s.memes {
		if s.memes[i].ID != id {
			continue
		}
		s.memes[i].SuggestedTags = normalizeTags(tags)
		if len(s.memes[i].SuggestedTags) > 0 {
			s.memes[i].AutoSuggestDisabled = false
		}
		s.memes[i].UpdatedAt = time.Now().UTC()
		s.byID[id] = s.memes[i]
		return nil
	}

	return os.ErrNotExist
}

// RemoveSuggestedTag drops one tag from a meme's suggestions under the store
// mutex, working on the list as it is now rather than on a caller's copy.
func (s *MemeStore) RemoveSuggestedTag(id, tag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	tag = strings.ToLower(strings.TrimSpace(tag))
	for i := range s.memes {
		if s.memes[i].ID != id {
			continue
		}
		kept := make([]string, 0, len(s.memes[i].SuggestedTags))
		for _, suggested := range s.memes[i].SuggestedTags {
			if suggested != tag {
				kept = append(kept, suggested)
			}
		}
		if len(kept) != len(s.memes[i].SuggestedTags) {
			s.memes[i].SuggestedTags = kept
			s.memes[i].UpdatedAt = time.Now().UTC()
			s.byID[id] = s.memes[i]
		}
		return nil
	}

	return os.ErrNotExist
}

func (s *MemeStore) Random(excludedIDs []string) (accessor.Meme, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.memes) == 0 {
		return accessor.Meme{}, os.ErrNotExist
	}

	excluded := make(map[string]struct{}, len(excludedIDs))
	for _, id := range excludedIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		excluded[trimmed] = struct{}{}
	}

	candidates := make([]accessor.Meme, 0, len(s.memes))
	for _, meme := range s.memes {
		if _, skip := excluded[meme.ID]; skip {
			continue
		}
		candidates = append(candidates, meme)
	}

	if len(candidates) == 0 {
		candidates = s.memes
	}

	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(candidates))))
	if err != nil {
		return accessor.Meme{}, fmt.Errorf("pick random meme: %w", err)
	}

	return candidates[index.Int64()], nil
}

// newMemeID is accessor.NewID for new memes. A test swaps it to force a clash
// with a meme that already exists.
var newMemeID = accessor.NewID

func (s *MemeStore) Create(input accessor.CreateInput) (accessor.Meme, error) {
	id := newMemeID()

	contentType := detectContentType(input.Header, input.ContentType, input.Filename)
	storedName := id + accessor.StoredFileExtension(input.Filename, contentType)
	targetPath := filepath.Join(s.uploadDir, storedName)

	// O_EXCL: never write over a file that is already there. A new ID cannot
	// name an existing file, but if it ever did (see NewID), this upload fails
	// instead of replacing that meme's file and then deleting it on cleanup.
	dst, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return accessor.Meme{}, fmt.Errorf("create upload file: %w", err)
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(dst, hasher), input.File)
	if err := errors.Join(copyErr, dst.Close()); err != nil {
		_ = os.Remove(targetPath)
		return accessor.Meme{}, fmt.Errorf("write upload file: %w", err)
	}

	now := time.Now().UTC()
	meme := accessor.Meme{
		ID:           id,
		OriginalName: input.Filename,
		StoredName:   storedName,
		FilePath:     "/uploads/" + storedName,
		ContentType:  contentType,
		ContentHash:  hex.EncodeToString(hasher.Sum(nil)),
		SizeBytes:    size,
		Tags:         normalizeTags(input.Tags),
		Notes:        strings.TrimSpace(input.Notes),
		SourceURL:    strings.TrimSpace(input.SourceURL),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.byHash[meme.ContentHash]; ok {
		_ = os.Remove(targetPath)
		existing.Favorite = isFavorite(s.favoriteSetLocked(""), existing.ID)
		return accessor.Meme{}, &accessor.DuplicateMemeError{Existing: existing}
	}

	s.memes = append(s.memes, meme)
	s.addToIndexesLocked(meme)
	s.decoratePreviewPath(&meme)
	return meme, nil
}

func (s *MemeStore) SetAutoSuggestDisabled(id string, disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	for i := range s.memes {
		if s.memes[i].ID != id {
			continue
		}
		s.memes[i].AutoSuggestDisabled = disabled
		s.memes[i].UpdatedAt = time.Now().UTC()
		s.byID[id] = s.memes[i]
		return nil
	}

	return os.ErrNotExist
}

// EditMeme applies a partial edit under the store mutex, which serializes it
// with every other write just as the row lock does in PostgresStore.
func (s *MemeStore) EditMeme(userID, id string, edit accessor.MemeEdit) (accessor.Meme, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	for i := range s.memes {
		if s.memes[i].ID != id {
			continue
		}

		current := s.memes[i]
		current.Favorite = isFavorite(s.favoriteSetLocked(userID), current.ID)
		change, err := accessor.PlanMemeEdit(current, edit)
		if err != nil {
			return accessor.Meme{}, err
		}
		if edit.Authorize != nil {
			if err := edit.Authorize(change); err != nil {
				return accessor.Meme{}, err
			}
		}
		if change.Empty() {
			return current, nil
		}

		if change.Notes != nil || len(change.TagsAdded) > 0 || len(change.TagsRemoved) > 0 {
			s.removeFromIndexesLocked(s.memes[i])
			s.memes[i].Tags = change.ApplyToTags(s.memes[i].Tags)
			if change.Notes != nil {
				s.memes[i].Notes = *change.Notes
			}
			s.memes[i].UpdatedAt = time.Now().UTC()
			s.addToIndexesLocked(s.memes[i])
		}
		if change.Favorite != nil {
			s.setFavoriteLocked(userID, s.memes[i].ID, *change.Favorite)
		}

		out := s.memes[i]
		out.Favorite = isFavorite(s.favoriteSetLocked(userID), out.ID)
		return out, nil
	}

	return accessor.Meme{}, os.ErrNotExist
}

func (s *MemeStore) SetFavorite(userID, id string, favorite bool) (accessor.Meme, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.memes {
		if s.memes[i].ID != id {
			continue
		}

		s.setFavoriteLocked(userID, s.memes[i].ID, favorite)
		out := s.memes[i]
		out.Favorite = isFavorite(s.favoriteSetLocked(userID), out.ID)
		return out, nil
	}

	return accessor.Meme{}, os.ErrNotExist
}

func (s *MemeStore) TotalFavoriteAssignments() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := 0
	for _, favorites := range s.favoritesByUser {
		total += len(favorites)
	}
	return total, nil
}

// FavoriteActivitySince is always empty: the store keeps no audit timeline,
// and an empty series keeps the current favorite total accurate without
// inventing historical changes.
func (s *MemeStore) FavoriteActivitySince(time.Time) ([]accessor.AdminFavoriteActivity, error) {
	return []accessor.AdminFavoriteActivity{}, nil
}

// CountFavoritesByUser returns each user's favorite count.
func (s *MemeStore) CountFavoritesByUser() (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[string]int, len(s.favoritesByUser))
	for userID, favorites := range s.favoritesByUser {
		counts[userID] = len(favorites)
	}
	return counts, nil
}

func (s *MemeStore) Delete(input accessor.DeleteInput) (accessor.DeleteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := strings.TrimSpace(input.ID)

	for i := range s.memes {
		if s.memes[i].ID != id {
			continue
		}

		s.removeFromIndexesLocked(s.memes[i])
		targetPath := filepath.Join(s.uploadDir, s.memes[i].StoredName)
		if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return accessor.DeleteResult{}, fmt.Errorf("remove upload file: %w", err)
		}
		thumbnailPath := filepath.Join(s.previewDir, thumbnailFileName(s.memes[i].StoredName))
		if err := os.Remove(thumbnailPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return accessor.DeleteResult{}, fmt.Errorf("remove thumbnail file: %w", err)
		}

		s.removeFavoriteFromAllUsersLocked(s.memes[i].ID)
		s.memes = append(s.memes[:i], s.memes[i+1:]...)
		return accessor.DeleteResult{Deleted: true}, nil
	}

	return accessor.DeleteResult{}, os.ErrNotExist
}

func (s *MemeStore) UploadDir() string {
	return s.uploadDir
}

func (s *MemeStore) ThumbnailDir() string {
	return s.previewDir
}

// EnsurePreviewAssets does nothing: this store never generates thumbnails.
func (s *MemeStore) EnsurePreviewAssets(func(work func()) bool) error {
	return nil
}

// AllStoredFiles returns every meme's file names (nothing is ever hidden).
func (s *MemeStore) AllStoredFiles() ([]accessor.StoredFile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]accessor.StoredFile, 0, len(s.memes))
	for _, meme := range s.memes {
		out = append(out, accessor.StoredFile{ID: meme.ID, Title: meme.OriginalName, StoredName: meme.StoredName, FilePath: meme.FilePath, SizeBytes: meme.SizeBytes})
	}
	return out, nil
}

// decoratePreviewPath sets the preview path PostgresStore would report for a
// meme with the thumbnails currently on disk: an image previews as its
// thumbnail or else itself, a video only as its thumbnail.
func (s *MemeStore) decoratePreviewPath(meme *accessor.Meme) {
	thumbnail := ""
	if _, err := os.Stat(filepath.Join(s.previewDir, thumbnailFileName(meme.StoredName))); err == nil {
		thumbnail = "/thumbnails/" + thumbnailFileName(meme.StoredName)
	}
	switch {
	case strings.HasPrefix(meme.ContentType, "image/"):
		meme.PreviewPath = meme.FilePath
		if thumbnail != "" {
			meme.PreviewPath = thumbnail
		}
	case strings.HasPrefix(meme.ContentType, "video/"):
		meme.PreviewPath = thumbnail
	default:
		meme.PreviewPath = ""
	}
}

func thumbnailFileName(storedName string) string {
	return strings.TrimSuffix(storedName, filepath.Ext(storedName)) + ".jpg"
}

func matchesQuery(meme accessor.Meme, needle string) bool {
	if strings.Contains(strings.ToLower(meme.OriginalName), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(meme.Notes), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(meme.SourceURL), needle) ||
		strings.Contains(strings.ToLower(meme.ContentType), needle) ||
		strings.Contains(strings.ToLower(filepath.Ext(meme.OriginalName)), needle) {
		return true
	}
	for _, tag := range meme.Tags {
		if strings.Contains(strings.ToLower(tag), needle) {
			return true
		}
	}
	return false
}

func (s *MemeStore) addToIndexesLocked(meme accessor.Meme) {
	s.byID[meme.ID] = meme
	if meme.ContentHash != "" {
		s.byHash[meme.ContentHash] = meme
	}
	for _, tag := range meme.Tags {
		bucket, ok := s.tagIndex[tag]
		if !ok {
			bucket = map[string]struct{}{}
			s.tagIndex[tag] = bucket
		}
		bucket[meme.ID] = struct{}{}
	}
}

func (s *MemeStore) removeFromIndexesLocked(meme accessor.Meme) {
	delete(s.byID, meme.ID)
	if meme.ContentHash != "" {
		delete(s.byHash, meme.ContentHash)
	}
	for _, tag := range meme.Tags {
		bucket, ok := s.tagIndex[tag]
		if !ok {
			continue
		}
		delete(bucket, meme.ID)
		if len(bucket) == 0 {
			delete(s.tagIndex, tag)
		}
	}
}

func (s *MemeStore) memesMatchingTagLocked(tag string) []accessor.Meme {
	out := make([]accessor.Meme, 0)
	seen := make(map[string]struct{})

	for indexedTag, ids := range s.tagIndex {
		if !strings.Contains(indexedTag, tag) {
			continue
		}

		for id := range ids {
			if _, exists := seen[id]; exists {
				continue
			}

			meme, ok := s.byID[id]
			if !ok {
				continue
			}

			seen[id] = struct{}{}
			out = append(out, meme)
		}
	}

	return out
}

func normalizeFavoriteUserID(userID string) string {
	trimmed := strings.TrimSpace(userID)
	if trimmed == "" {
		return localFavoritesUserID
	}
	return trimmed
}

func (s *MemeStore) favoriteSetLocked(userID string) map[string]struct{} {
	return s.favoritesByUser[normalizeFavoriteUserID(userID)]
}

func isFavorite(bucket map[string]struct{}, memeID string) bool {
	_, ok := bucket[memeID]
	return ok
}

func (s *MemeStore) setFavoriteLocked(userID, memeID string, favorite bool) {
	normalizedUserID := normalizeFavoriteUserID(userID)
	bucket, ok := s.favoritesByUser[normalizedUserID]
	if !ok {
		bucket = map[string]struct{}{}
		s.favoritesByUser[normalizedUserID] = bucket
	}

	if favorite {
		bucket[memeID] = struct{}{}
		return
	}

	delete(bucket, memeID)
	if len(bucket) == 0 {
		delete(s.favoritesByUser, normalizedUserID)
	}
}

func (s *MemeStore) removeFavoriteFromAllUsersLocked(memeID string) {
	for userID, bucket := range s.favoritesByUser {
		if _, ok := bucket[memeID]; !ok {
			continue
		}
		delete(bucket, memeID)
		if len(bucket) == 0 {
			delete(s.favoritesByUser, userID)
		}
	}
}

// normalizeTags and detectContentType mirror the accessor helpers of the same
// name, which PostgresStore applies on create and update.
func normalizeTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		normalized := normalizeTag(tag)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func normalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

func detectContentType(header textproto.MIMEHeader, explicitContentType, filename string) string {
	if strings.TrimSpace(explicitContentType) != "" {
		return explicitContentType
	}
	if contentType := header.Get("Content-Type"); contentType != "" {
		return contentType
	}
	if ext := filepath.Ext(filename); ext != "" {
		if guessed := mime.TypeByExtension(ext); guessed != "" {
			return guessed
		}
	}
	return "application/octet-stream"
}
