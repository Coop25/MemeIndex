package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"memeindex/internal/accessor"
)

const (
	reelSessionTTL         = 48 * time.Hour
	reelRecentExclusionCap = 100
)

type ReelStepResult struct {
	SessionID       string
	SessionReplaced bool
	Reason          string
	CanGoPrev       bool
	Meme            accessor.Meme
}

type reelSession struct {
	History      []string
	Position     int
	LastActivity time.Time
}

type ReelSessionStore struct {
	mu          sync.Mutex
	sessions    map[string]*reelSession
	backend     accessor.ReelSessionPersistence
	store       accessor.Store
	sessionFile string
}

func NewReelSessionStore(store accessor.Store, sessionFile string) *ReelSessionStore {
	out := &ReelSessionStore{
		sessions:    map[string]*reelSession{},
		backend:     reelSessionBackend(store),
		store:       store,
		sessionFile: sessionFile,
	}
	if err := out.load(); err != nil {
		// Keep startup resilient; sessions are disposable.
		fmt.Printf("could not load reel sessions: %v\n", err)
	}
	return out
}

func (s *ReelSessionStore) Step(sessionID string, direction string) (ReelStepResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.cleanupLocked(); err != nil {
		return ReelStepResult{}, err
	}

	now := time.Now().UTC()
	sessionID = strings.TrimSpace(sessionID)
	direction = strings.ToLower(strings.TrimSpace(direction))
	if direction == "" {
		direction = "next"
	}

	replaced := false
	reason := ""
	session, ok := s.sessions[sessionID]
	if !ok || sessionID == "" {
		if sessionID != "" {
			replaced = true
			reason = "missing"
		}
		var err error
		sessionID, session, err = s.newSessionLocked(now)
		if err != nil {
			return ReelStepResult{}, err
		}
	} else if now.Sub(session.LastActivity) > reelSessionTTL {
		delete(s.sessions, sessionID)
		replaced = true
		reason = "stale"
		var err error
		sessionID, session, err = s.newSessionLocked(now)
		if err != nil {
			return ReelStepResult{}, err
		}
	}

	switch direction {
	case "prev":
		if session.Position > 0 {
			session.Position -= 1
		}
	case "next":
		if session.Position < len(session.History)-1 {
			session.Position += 1
		} else {
			excluded := s.recentHistoryLocked(session)
			meme, err := s.store.Random(excluded)
			if err != nil {
				return ReelStepResult{}, err
			}
			session.History = append(session.History, meme.ID)
			session.Position = len(session.History) - 1
		}
	default:
		return ReelStepResult{}, fmt.Errorf("unknown direction: %s", direction)
	}

	session.LastActivity = now
	if err := s.saveLocked(); err != nil {
		return ReelStepResult{}, err
	}
	memeID := session.History[session.Position]
	meme, err := s.store.GetByID("", memeID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			delete(s.sessions, sessionID)
			if s.backend != nil {
				_ = s.backend.DeleteReelSession(sessionID)
			}
		}
		return ReelStepResult{}, err
	}

	return ReelStepResult{
		SessionID:       sessionID,
		SessionReplaced: replaced,
		Reason:          reason,
		CanGoPrev:       session.Position > 0,
		Meme:            meme,
	}, nil
}

func (s *ReelSessionStore) Delete(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	if _, ok := s.sessions[sessionID]; !ok {
		return nil
	}

	delete(s.sessions, sessionID)
	if s.backend != nil {
		return s.backend.DeleteReelSession(sessionID)
	}
	return s.saveLocked()
}

func (s *ReelSessionStore) CleanupStale() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleanupLocked()
}

func (s *ReelSessionStore) newSessionLocked(now time.Time) (string, *reelSession, error) {
	id, err := randomSessionID()
	if err != nil {
		return "", nil, err
	}

	meme, err := s.store.Random(nil)
	if err != nil {
		return "", nil, err
	}

	session := &reelSession{
		History:      []string{meme.ID},
		Position:     0,
		LastActivity: now,
	}
	s.sessions[id] = session
	return id, session, nil
}

func (s *ReelSessionStore) cleanupLocked() error {
	if s.backend != nil {
		before := time.Now().UTC().Add(-reelSessionTTL)
		if err := s.backend.CleanupStaleReelSessions(before); err != nil {
			return err
		}
		for id, session := range s.sessions {
			if session.LastActivity.Before(before) {
				delete(s.sessions, id)
			}
		}
		return nil
	}

	changed := false
	now := time.Now().UTC()
	for id, session := range s.sessions {
		if now.Sub(session.LastActivity) > reelSessionTTL {
			delete(s.sessions, id)
			changed = true
		}
	}
	if changed {
		return s.saveLocked()
	}
	return nil
}

func (s *ReelSessionStore) recentHistoryLocked(session *reelSession) []string {
	if len(session.History) == 0 {
		return nil
	}

	start := 0
	if len(session.History) > reelRecentExclusionCap {
		start = len(session.History) - reelRecentExclusionCap
	}
	return append([]string(nil), session.History[start:]...)
}

func randomSessionID() (string, error) {
	return uuid.Must(uuid.NewV6()).String(), nil
}

func (s *ReelSessionStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.backend != nil {
		records, err := s.backend.LoadReelSessions()
		if err != nil {
			return err
		}
		s.sessions = map[string]*reelSession{}
		for sessionID, record := range records {
			s.sessions[sessionID] = &reelSession{
				History:      append([]string(nil), record.History...),
				Position:     record.Position,
				LastActivity: record.LastActivity,
			}
		}
		return s.cleanupLocked()
	}

	if strings.TrimSpace(s.sessionFile) == "" {
		return nil
	}

	payload, err := os.ReadFile(s.sessionFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read reel sessions: %w", err)
	}
	if len(payload) == 0 {
		return nil
	}

	var sessions map[string]*reelSession
	if err := json.Unmarshal(payload, &sessions); err != nil {
		return fmt.Errorf("decode reel sessions: %w", err)
	}
	if sessions == nil {
		sessions = map[string]*reelSession{}
	}
	s.sessions = sessions
	return s.cleanupLocked()
}

func (s *ReelSessionStore) saveLocked() error {
	if s.backend != nil {
		for sessionID, session := range s.sessions {
			if err := s.backend.SaveReelSession(sessionID, accessor.ReelSessionRecord{
				History:      append([]string(nil), session.History...),
				Position:     session.Position,
				LastActivity: session.LastActivity,
			}); err != nil {
				return err
			}
		}
		return nil
	}

	if strings.TrimSpace(s.sessionFile) == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.sessionFile), 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	payload, err := json.MarshalIndent(s.sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("encode reel sessions: %w", err)
	}
	if err := os.WriteFile(s.sessionFile, payload, 0o644); err != nil {
		return fmt.Errorf("write reel sessions: %w", err)
	}
	return nil
}

func reelSessionBackend(store accessor.Store) accessor.ReelSessionPersistence {
	backend, ok := store.(accessor.ReelSessionPersistence)
	if !ok {
		return nil
	}
	return backend
}
