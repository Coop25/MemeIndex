package accessor

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"net/textproto"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Meme struct {
	ID           string    `json:"id"`
	OriginalName string    `json:"originalName"`
	StoredName   string    `json:"storedName"`
	FilePath     string    `json:"filePath"`
	PreviewPath  string    `json:"previewPath,omitempty"`
	ContentType  string    `json:"contentType"`
	ContentHash  string    `json:"-"`
	SizeBytes    int64     `json:"sizeBytes"`
	Tags         []string  `json:"tags"`
	Notes        string    `json:"notes"`
	Favorite     bool      `json:"favorite"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type persistedMeme struct {
	ID           string    `json:"id"`
	OriginalName string    `json:"originalName"`
	StoredName   string    `json:"storedName"`
	FilePath     string    `json:"filePath"`
	ContentType  string    `json:"contentType"`
	ContentHash  string    `json:"contentHash,omitempty"`
	SizeBytes    int64     `json:"sizeBytes"`
	Tags         []string  `json:"tags"`
	Notes        string    `json:"notes"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type MemeUpdate struct {
	Tags     []string `json:"tags"`
	Notes    string   `json:"notes"`
	Favorite bool     `json:"favorite"`
}

type CreateInput struct {
	File        io.Reader
	Header      textproto.MIMEHeader
	Filename    string
	Tags        []string
	Notes       string
	ContentType string
}

type MemeStore struct {
	mu              sync.RWMutex
	dataFile        string
	favoritesFile   string
	uploadDir       string
	previewDir      string
	memes           []Meme
	byID            map[string]Meme
	byHash          map[string]Meme
	tagIndex        map[string]map[string]struct{}
	favoritesByUser map[string]map[string]struct{}
}

const localFavoritesUserID = "__local__"

func NewMemeStore(dataDir string) (*MemeStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	uploadDir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}

	store := &MemeStore{
		dataFile:        filepath.Join(dataDir, "index.json"),
		favoritesFile:   filepath.Join(dataDir, "favorites.json"),
		uploadDir:       uploadDir,
		previewDir:      filepath.Join(dataDir, "thumbnails"),
		memes:           []Meme{},
		byID:            map[string]Meme{},
		byHash:          map[string]Meme{},
		tagIndex:        map[string]map[string]struct{}{},
		favoritesByUser: map[string]map[string]struct{}{},
	}

	if err := store.loadMemes(); err != nil {
		return nil, err
	}
	if err := store.loadFavorites(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *MemeStore) List(userID, query string, favoritesOnly bool, tag string) []Meme {
	s.mu.RLock()
	defer s.mu.RUnlock()

	needle := strings.ToLower(strings.TrimSpace(query))
	tag = normalizeTag(tag)
	favorites := s.favoriteSetLocked(userID)
	candidates := s.memes
	if tag != "" {
		candidates = s.memesMatchingTagLocked(tag)
	}

	out := make([]Meme, 0, len(candidates))
	for _, meme := range candidates {
		isFavorite := s.isFavoriteLocked(favorites, meme.ID)
		if favoritesOnly && !isFavorite {
			continue
		}
		if needle != "" && !matchesQuery(meme, needle) {
			continue
		}

		meme.Favorite = isFavorite
		decoratePreviewPath(&meme, s.previewDir)
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

	out := make([]string, 0, limit)
	for tag := range s.tagIndex {
		if prefix != "" && !strings.Contains(tag, prefix) {
			continue
		}
		out = append(out, tag)
	}

	sort.Strings(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *MemeStore) GetByID(userID, id string) (Meme, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meme, ok := s.byID[strings.TrimSpace(id)]
	if !ok {
		return Meme{}, os.ErrNotExist
	}
	meme.Favorite = s.isFavoriteLocked(s.favoriteSetLocked(userID), meme.ID)
	decoratePreviewPath(&meme, s.previewDir)
	return meme, nil
}

func (s *MemeStore) Random(excludedIDs []string) (Meme, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.memes) == 0 {
		return Meme{}, os.ErrNotExist
	}

	excluded := make(map[string]struct{}, len(excludedIDs))
	for _, id := range excludedIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		excluded[trimmed] = struct{}{}
	}

	candidates := make([]Meme, 0, len(s.memes))
	for _, meme := range s.memes {
		if _, skip := excluded[meme.ID]; skip {
			continue
		}
		candidates = append(candidates, meme)
	}

	if len(candidates) == 0 {
		candidates = s.memes
	}

	max := big.NewInt(int64(len(candidates)))
	index, err := rand.Int(rand.Reader, max)
	if err != nil {
		return Meme{}, fmt.Errorf("pick random meme: %w", err)
	}

	return candidates[index.Int64()], nil
}

func (s *MemeStore) Create(input CreateInput) (Meme, error) {
	id := uuid.Must(uuid.NewV6()).String()

	ext := filepath.Ext(input.Filename)
	storedName := id + ext
	targetPath := filepath.Join(s.uploadDir, storedName)

	dst, err := os.Create(targetPath)
	if err != nil {
		return Meme{}, fmt.Errorf("create upload file: %w", err)
	}
	defer dst.Close()

	hasher := newContentHashWriter()
	size, err := io.Copy(io.MultiWriter(dst, hasher), input.File)
	if err != nil {
		return Meme{}, fmt.Errorf("write upload file: %w", err)
	}

	now := time.Now().UTC()
	meme := Meme{
		ID:           id,
		OriginalName: input.Filename,
		StoredName:   storedName,
		FilePath:     "/uploads/" + storedName,
		ContentType:  detectContentType(input.Header, input.ContentType, input.Filename),
		ContentHash:  contentHashString(hasher),
		SizeBytes:    size,
		Tags:         normalizeTags(input.Tags),
		Notes:        strings.TrimSpace(input.Notes),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := ensurePreviewAsset(s.uploadDir, s.previewDir, &meme); err != nil {
		// Thumbnail generation is best-effort.
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.byHash[meme.ContentHash]; ok {
		_ = os.Remove(targetPath)
		existing.Favorite = s.isFavoriteLocked(s.favoriteSetLocked(""), existing.ID)
		return Meme{}, &DuplicateMemeError{Existing: existing}
	}

	s.memes = append(s.memes, meme)
	s.addToIndexesLocked(meme)
	if err := s.saveMemesLocked(); err != nil {
		return Meme{}, err
	}

	return meme, nil
}

func (s *MemeStore) Update(userID, id string, update MemeUpdate) (Meme, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.memes {
		if s.memes[i].ID != id {
			continue
		}

		s.removeFromIndexesLocked(s.memes[i])
		s.memes[i].Tags = normalizeTags(update.Tags)
		s.memes[i].Notes = strings.TrimSpace(update.Notes)
		s.memes[i].UpdatedAt = time.Now().UTC()
		s.addToIndexesLocked(s.memes[i])
		s.setFavoriteLocked(userID, s.memes[i].ID, update.Favorite)

		if err := s.saveMemesLocked(); err != nil {
			return Meme{}, err
		}
		if err := s.saveFavoritesLocked(); err != nil {
			return Meme{}, err
		}

		out := s.memes[i]
		out.Favorite = s.isFavoriteLocked(s.favoriteSetLocked(userID), out.ID)
		return out, nil
	}

	return Meme{}, os.ErrNotExist
}

func (s *MemeStore) SetFavorite(userID, id string, favorite bool) (Meme, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.memes {
		if s.memes[i].ID != id {
			continue
		}

		s.setFavoriteLocked(userID, s.memes[i].ID, favorite)
		if err := s.saveFavoritesLocked(); err != nil {
			return Meme{}, err
		}

		out := s.memes[i]
		out.Favorite = s.isFavoriteLocked(s.favoriteSetLocked(userID), out.ID)
		return out, nil
	}

	return Meme{}, os.ErrNotExist
}

func (s *MemeStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.memes {
		if s.memes[i].ID != id {
			continue
		}

		s.removeFromIndexesLocked(s.memes[i])
		targetPath := filepath.Join(s.uploadDir, s.memes[i].StoredName)
		if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove upload file: %w", err)
		}
		thumbnailPath := filepath.Join(s.previewDir, thumbnailFileName(s.memes[i].StoredName))
		if err := os.Remove(thumbnailPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove thumbnail file: %w", err)
		}

		favoritesChanged := s.removeFavoriteFromAllUsersLocked(s.memes[i].ID)
		s.memes = append(s.memes[:i], s.memes[i+1:]...)
		if err := s.saveMemesLocked(); err != nil {
			return err
		}
		if favoritesChanged {
			return s.saveFavoritesLocked()
		}
		return nil
	}

	return os.ErrNotExist
}

func (s *MemeStore) UploadDir() string {
	return s.uploadDir
}

func (s *MemeStore) ThumbnailDir() string {
	return s.previewDir
}

func (s *MemeStore) EnsurePreviewAssets() error {
	s.mu.RLock()
	memes := append([]Meme(nil), s.memes...)
	s.mu.RUnlock()

	for i := range memes {
		if err := ensurePreviewAsset(s.uploadDir, s.previewDir, &memes[i]); err != nil {
			continue
		}
	}
	return nil
}

func (s *MemeStore) loadMemes() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := os.ReadFile(s.dataFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read index: %w", err)
	}

	if len(payload) == 0 {
		return nil
	}

	var persisted []persistedMeme
	if err := json.Unmarshal(payload, &persisted); err != nil {
		return fmt.Errorf("decode index: %w", err)
	}
	s.memes = make([]Meme, 0, len(persisted))
	for _, item := range persisted {
		s.memes = append(s.memes, Meme{
			ID:           item.ID,
			OriginalName: item.OriginalName,
			StoredName:   item.StoredName,
			FilePath:     item.FilePath,
			ContentType:  item.ContentType,
			ContentHash:  item.ContentHash,
			SizeBytes:    item.SizeBytes,
			Tags:         item.Tags,
			Notes:        item.Notes,
			CreatedAt:    item.CreatedAt,
			UpdatedAt:    item.UpdatedAt,
		})
	}

	s.rebuildIndexesLocked()

	return nil
}

func (s *MemeStore) loadFavorites() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := os.ReadFile(s.favoritesFile)
	if errors.Is(err, os.ErrNotExist) {
		s.favoritesByUser = map[string]map[string]struct{}{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read favorites: %w", err)
	}
	if len(payload) == 0 {
		s.favoritesByUser = map[string]map[string]struct{}{}
		return nil
	}

	var raw map[string][]string
	if err := json.Unmarshal(payload, &raw); err != nil {
		return fmt.Errorf("decode favorites: %w", err)
	}

	s.favoritesByUser = map[string]map[string]struct{}{}
	for userID, memeIDs := range raw {
		normalizedUserID := normalizeFavoriteUserID(userID)
		bucket := map[string]struct{}{}
		for _, memeID := range memeIDs {
			trimmedID := strings.TrimSpace(memeID)
			if trimmedID == "" {
				continue
			}
			bucket[trimmedID] = struct{}{}
		}
		if len(bucket) > 0 {
			s.favoritesByUser[normalizedUserID] = bucket
		}
	}
	return nil
}

func (s *MemeStore) saveMemesLocked() error {
	persisted := make([]persistedMeme, 0, len(s.memes))
	for _, meme := range s.memes {
		persisted = append(persisted, persistedMeme{
			ID:           meme.ID,
			OriginalName: meme.OriginalName,
			StoredName:   meme.StoredName,
			FilePath:     meme.FilePath,
			ContentType:  meme.ContentType,
			ContentHash:  meme.ContentHash,
			SizeBytes:    meme.SizeBytes,
			Tags:         meme.Tags,
			Notes:        meme.Notes,
			CreatedAt:    meme.CreatedAt,
			UpdatedAt:    meme.UpdatedAt,
		})
	}

	payload, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("encode index: %w", err)
	}

	if err := os.WriteFile(s.dataFile, payload, 0o644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	return nil
}

func (s *MemeStore) saveFavoritesLocked() error {
	raw := map[string][]string{}
	for userID, bucket := range s.favoritesByUser {
		if len(bucket) == 0 {
			continue
		}

		memeIDs := make([]string, 0, len(bucket))
		for memeID := range bucket {
			if _, exists := s.byID[memeID]; !exists {
				continue
			}
			memeIDs = append(memeIDs, memeID)
		}
		if len(memeIDs) == 0 {
			continue
		}
		sort.Strings(memeIDs)
		raw[userID] = memeIDs
	}

	payload, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encode favorites: %w", err)
	}
	if err := os.WriteFile(s.favoritesFile, payload, 0o644); err != nil {
		return fmt.Errorf("write favorites: %w", err)
	}
	return nil
}

func matchesQuery(meme Meme, needle string) bool {
	if strings.Contains(strings.ToLower(meme.OriginalName), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(meme.Notes), needle) {
		return true
	}
	for _, tag := range meme.Tags {
		if strings.Contains(strings.ToLower(tag), needle) {
			return true
		}
	}
	return false
}

func (s *MemeStore) rebuildIndexesLocked() {
	s.byID = make(map[string]Meme, len(s.memes))
	s.byHash = make(map[string]Meme, len(s.memes))
	s.tagIndex = map[string]map[string]struct{}{}
	hashesBackfilled := false
	for i, meme := range s.memes {
		if strings.TrimSpace(meme.ContentHash) == "" {
			hash, err := computeFileHash(filepath.Join(s.uploadDir, meme.StoredName))
			if err == nil {
				meme.ContentHash = hash
				s.memes[i].ContentHash = hash
				hashesBackfilled = true
			}
		}
		s.addToIndexesLocked(meme)
	}
	if hashesBackfilled {
		_ = s.saveMemesLocked()
	}
}

func (s *MemeStore) addToIndexesLocked(meme Meme) {
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

func (s *MemeStore) removeFromIndexesLocked(meme Meme) {
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

func (s *MemeStore) memesMatchingTagLocked(tag string) []Meme {
	out := make([]Meme, 0)
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

func (s *MemeStore) isFavoriteLocked(bucket map[string]struct{}, memeID string) bool {
	if len(bucket) == 0 {
		return false
	}
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

func (s *MemeStore) removeFavoriteFromAllUsersLocked(memeID string) bool {
	changed := false
	for userID, bucket := range s.favoritesByUser {
		if _, ok := bucket[memeID]; !ok {
			continue
		}
		delete(bucket, memeID)
		changed = true
		if len(bucket) == 0 {
			delete(s.favoritesByUser, userID)
		}
	}
	return changed
}

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
