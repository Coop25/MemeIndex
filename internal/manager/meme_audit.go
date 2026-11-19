package manager

import (
	"context"
	"log"
	"memeindex/internal/accessor"
	"strings"
)

func (m *MemeManager) ListMemeAudit(ctx context.Context, id string, limit int) ([]accessor.MemeAuditEntry, error) {
	store, ok := m.store.(accessor.AuditStore)
	if !ok {
		return nil, nil
	}
	return store.ListMemeAudit(ctx, strings.TrimSpace(id), limit)
}

func (m *MemeManager) ListAuditFeed(ctx context.Context, offset int, limit int, actorUserID string) (accessor.PagedAuditFeed, error) {
	store, ok := m.store.(accessor.AuditStore)
	if !ok {
		return accessor.PagedAuditFeed{}, nil
	}
	return store.ListAuditFeed(ctx, offset, limit, actorUserID)
}

func (m *MemeManager) ListPendingDeletes(ctx context.Context, offset int, limit int) (accessor.PagedPendingDeletes, error) {
	store, ok := m.store.(accessor.AuditStore)
	if !ok {
		return accessor.PagedPendingDeletes{}, nil
	}
	return store.ListPendingDeletes(ctx, offset, limit)
}

func (m *MemeManager) ApprovePendingDelete(ctx context.Context, id string, actor accessor.AuditActor) error {
	store, ok := m.store.(accessor.AuditStore)
	if !ok {
		return nil
	}
	defer m.invalidateHiddenMemes()
	return store.ApprovePendingDelete(ctx, strings.TrimSpace(id), actor)
}

func (m *MemeManager) RejectPendingDelete(ctx context.Context, id string, actor accessor.AuditActor) error {
	store, ok := m.store.(accessor.AuditStore)
	if !ok {
		return nil
	}
	defer m.invalidateHiddenMemes()
	return store.RejectPendingDelete(ctx, strings.TrimSpace(id), actor)
}

// IsMemeHidden reports whether the meme with this id is hidden from the app
// (a delete request awaiting approval). Stores that never hide memes always
// answer false. A lookup failure fails closed for that one call and is retried
// on the next.
func (m *MemeManager) IsMemeHidden(ctx context.Context, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	store, ok := m.store.(accessor.AdminMemeStore)
	if !ok {
		return false
	}

	m.hiddenMemeMu.Lock()
	defer m.hiddenMemeMu.Unlock()
	if m.hiddenMemeIDs == nil {
		ids, err := store.HiddenMemeIDs(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("load hidden meme ids failed: %v", err)
			}
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
