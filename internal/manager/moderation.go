package manager

import (
	"log"
	"strings"

	"memeindex/internal/accessor"
)

func (m *MemeManager) ListMemeAudit(id string, limit int) ([]accessor.MemeAuditEntry, error) {
	store, ok := m.store.(accessor.AuditLogStore)
	if !ok {
		return nil, nil
	}
	return store.ListMemeAudit(strings.TrimSpace(id), limit)
}

func (m *MemeManager) ListAuditFeed(offset int, limit int, actorUserID string) (accessor.PagedAuditFeed, error) {
	store, ok := m.store.(accessor.AuditLogStore)
	if !ok {
		return accessor.PagedAuditFeed{}, nil
	}
	return store.ListAuditFeed(offset, limit, actorUserID)
}

func (m *MemeManager) ListPendingDeletes(offset int, limit int) (accessor.PagedPendingDeletes, error) {
	store, ok := m.store.(accessor.AuditLogStore)
	if !ok {
		return accessor.PagedPendingDeletes{}, nil
	}
	return store.ListPendingDeletes(offset, limit)
}

func (m *MemeManager) ApprovePendingDelete(id string, actor accessor.AuditActor) error {
	store, ok := m.store.(accessor.AuditLogStore)
	if !ok {
		return nil
	}
	defer m.invalidateHiddenMemes()
	return store.ApprovePendingDelete(strings.TrimSpace(id), actor)
}

func (m *MemeManager) RejectPendingDelete(id string, actor accessor.AuditActor) error {
	store, ok := m.store.(accessor.AuditLogStore)
	if !ok {
		return nil
	}
	defer m.invalidateHiddenMemes()
	return store.RejectPendingDelete(strings.TrimSpace(id), actor)
}

// IsMemeHidden reports whether the meme with this id is hidden from the app
// (a delete request awaiting approval). Stores that never hide memes always
// answer false. A lookup failure fails closed for that one call and is retried
// on the next.
func (m *MemeManager) IsMemeHidden(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	store, ok := m.store.(accessor.HiddenMemeStore)
	if !ok {
		return false
	}

	m.hiddenMemeMu.Lock()
	defer m.hiddenMemeMu.Unlock()
	if m.hiddenMemeIDs == nil {
		ids, err := store.HiddenMemeIDs()
		if err != nil {
			log.Printf("load hidden meme ids failed: %v", err)
			return true
		}
		m.hiddenMemeIDs = make(map[string]struct{}, len(ids))
		for _, hiddenID := range ids {
			m.hiddenMemeIDs[hiddenID] = struct{}{}
		}
	}
	_, hidden := m.hiddenMemeIDs[id]
	return hidden
}

func (m *MemeManager) invalidateHiddenMemes() {
	m.hiddenMemeMu.Lock()
	m.hiddenMemeIDs = nil
	m.hiddenMemeMu.Unlock()
}
