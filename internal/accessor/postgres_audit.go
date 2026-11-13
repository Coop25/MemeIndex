package accessor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) ListMemeAudit(id string, limit int) ([]MemeAuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.pool.Query(context.Background(), `
		SELECT id, meme_id, action, actor_user_id, actor_username, actor_display_name, actor_avatar_url, description, created_at
		FROM meme_audit_logs
		WHERE meme_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, strings.TrimSpace(id), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MemeAuditEntry{}
	for rows.Next() {
		var entry MemeAuditEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.MemeID,
			&entry.Action,
			&entry.Actor.UserID,
			&entry.Actor.Username,
			&entry.Actor.DisplayName,
			&entry.Actor.AvatarURL,
			&entry.Description,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListAuditFeed(offset int, limit int, actorUserID string) (PagedAuditFeed, error) {
	actorUserID = strings.TrimSpace(actorUserID) // empty matches every actor
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 250 {
		limit = 250
	}

	var total int
	if err := s.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM meme_audit_logs WHERE ($1 = '' OR actor_user_id = $1)`, actorUserID).Scan(&total); err != nil {
		return PagedAuditFeed{}, err
	}

	rows, err := s.pool.Query(context.Background(), `
		SELECT
			l.id,
			l.meme_id,
			l.action,
			l.actor_user_id,
			l.actor_username,
			l.actor_display_name,
			l.actor_avatar_url,
			l.description,
			l.created_at,
			COALESCE(m.original_name, '') AS meme_original_name,
			COALESCE(m.content_type, '') AS meme_content_type,
			COALESCE(m.file_path, '') AS meme_file_path
		FROM meme_audit_logs l
		LEFT JOIN memes m ON m.id = l.meme_id
		WHERE ($1 = '' OR l.actor_user_id = $1)
		ORDER BY l.created_at DESC, l.id DESC
		OFFSET $2
		LIMIT $3
	`, actorUserID, offset, limit)
	if err != nil {
		return PagedAuditFeed{}, err
	}
	defer rows.Close()

	out := []GlobalMemeAuditEntry{}
	for rows.Next() {
		var entry GlobalMemeAuditEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.MemeID,
			&entry.Action,
			&entry.Actor.UserID,
			&entry.Actor.Username,
			&entry.Actor.DisplayName,
			&entry.Actor.AvatarURL,
			&entry.Description,
			&entry.CreatedAt,
			&entry.MemeOriginalName,
			&entry.MemeContentType,
			&entry.MemeFilePath,
		); err != nil {
			return PagedAuditFeed{}, err
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return PagedAuditFeed{}, err
	}
	return PagedAuditFeed{
		Events:     out,
		Total:      total,
		HasMore:    offset+len(out) < total,
		NextOffset: min(offset+len(out), total),
	}, nil
}

func (s *PostgresStore) ListPendingDeletes(offset int, limit int) (PagedPendingDeletes, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var total int
	if err := s.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM memes WHERE COALESCE(pending_delete, FALSE) = TRUE`).Scan(&total); err != nil {
		return PagedPendingDeletes{}, err
	}

	rows, err := s.pool.Query(context.Background(), `
		SELECT
			m.id,
			m.original_name,
			m.stored_name,
			m.file_path,
			m.content_type,
			m.size_bytes,
			COALESCE(array_agg(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags,
			COALESCE(m.suggested_tags, '{}') AS suggested_tags,
			COALESCE(m.auto_suggest_disabled, FALSE) AS auto_suggest_disabled,
			m.notes,
			COALESCE(m.source_url, '') AS source_url,
			FALSE AS favorite,
			m.created_at,
			m.updated_at,
			COALESCE(u.user_id, m.delete_requested_by_user_id) AS requested_by_user_id,
			COALESCE(u.username, '') AS requested_by_username,
			COALESCE(u.display_name, '') AS requested_by_display_name,
			COALESCE(u.avatar_url, '') AS requested_by_avatar_url,
			m.delete_requested_at
		FROM memes m
		LEFT JOIN meme_tags mt ON mt.meme_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		LEFT JOIN app_users u ON u.user_id = m.delete_requested_by_user_id
		WHERE COALESCE(m.pending_delete, FALSE) = TRUE
		GROUP BY m.id, u.user_id, u.username, u.display_name, u.avatar_url, m.delete_requested_at
		ORDER BY m.delete_requested_at ASC NULLS LAST, m.created_at DESC
		OFFSET $1
		LIMIT $2
	`, offset, limit)
	if err != nil {
		return PagedPendingDeletes{}, err
	}
	defer rows.Close()

	out := []PendingDeleteRecord{}
	for rows.Next() {
		var record PendingDeleteRecord
		if err := rows.Scan(
			&record.Meme.ID,
			&record.Meme.OriginalName,
			&record.Meme.StoredName,
			&record.Meme.FilePath,
			&record.Meme.ContentType,
			&record.Meme.SizeBytes,
			&record.Meme.Tags,
			&record.Meme.Notes,
			&record.Meme.SourceURL,
			&record.Meme.Favorite,
			&record.Meme.CreatedAt,
			&record.Meme.UpdatedAt,
			&record.RequestedBy.UserID,
			&record.RequestedBy.Username,
			&record.RequestedBy.DisplayName,
			&record.RequestedBy.AvatarURL,
			&record.RequestedAt,
		); err != nil {
			return PagedPendingDeletes{}, err
		}
		decoratePreviewPath(&record.Meme, s.previewDir)
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return PagedPendingDeletes{}, err
	}
	return PagedPendingDeletes{
		Memes:      out,
		Total:      total,
		HasMore:    offset+len(out) < total,
		NextOffset: min(offset+len(out), total),
	}, nil
}

func (s *PostgresStore) ApprovePendingDelete(id string, actor AuditActor) error {
	ctx := context.Background()
	id = strings.TrimSpace(id)

	var storedName string
	err := s.pool.QueryRow(ctx, `SELECT stored_name FROM memes WHERE id = $1 AND COALESCE(pending_delete, FALSE) = TRUE`, id).Scan(&storedName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return os.ErrNotExist
		}
		return err
	}

	if err := s.insertAuditLog(ctx, s.pool, id, "delete_approved", actor, "Approved delete request"); err != nil {
		return err
	}
	orphanCandidates, err := scanInt64List(ctx, s.pool, `SELECT tag_id FROM meme_tags WHERE meme_id = $1`, id)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM memes WHERE id = $1`, id); err != nil {
		return err
	}
	if err := s.deleteOrphanTags(ctx, s.pool, orphanCandidates); err != nil {
		return err
	}

	targetPath := filepath.Join(s.uploadDir, storedName)
	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove upload file: %w", err)
	}
	thumbnailPath := filepath.Join(s.previewDir, thumbnailFileName(storedName))
	if err := os.Remove(thumbnailPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove thumbnail file: %w", err)
	}
	return nil
}

func (s *PostgresStore) RejectPendingDelete(id string, actor AuditActor) error {
	ctx := context.Background()
	commandTag, err := s.pool.Exec(ctx, `
		UPDATE memes
		SET hidden_from_app = FALSE,
			pending_delete = FALSE,
			delete_requested_by_user_id = '',
			delete_requested_at = NULL,
			updated_at = NOW()
		WHERE id = $1 AND COALESCE(pending_delete, FALSE) = TRUE
	`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return os.ErrNotExist
	}
	return s.insertAuditLog(ctx, s.pool, strings.TrimSpace(id), "delete_rejected", actor, "Rejected delete request")
}

// RecordSystemAudit writes an audit entry that is not associated with a specific
// meme. The empty meme_id is surfaced by ListAuditFeed's LEFT JOIN as a row with
// no meme attached.
func (s *PostgresStore) RecordSystemAudit(action string, actor AuditActor, description string) error {
	return s.insertAuditLog(context.Background(), s.pool, "", action, actor, description)
}

func (s *PostgresStore) insertAuditLog(ctx context.Context, db queryable, memeID, action string, actor AuditActor, description string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO meme_audit_logs (
			meme_id, action, actor_user_id, actor_username, actor_display_name, actor_avatar_url, description, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`, strings.TrimSpace(memeID), strings.TrimSpace(action), strings.TrimSpace(actor.UserID), strings.TrimSpace(actor.Username), strings.TrimSpace(actor.DisplayName), strings.TrimSpace(actor.AvatarURL), strings.TrimSpace(description))
	return err
}

func diffAuditTags(current []string, next []string) ([]string, []string) {
	currentSet := map[string]struct{}{}
	nextSet := map[string]struct{}{}
	for _, tag := range normalizeTags(current) {
		currentSet[tag] = struct{}{}
	}
	for _, tag := range normalizeTags(next) {
		nextSet[tag] = struct{}{}
	}

	added := []string{}
	removed := []string{}
	for tag := range nextSet {
		if _, ok := currentSet[tag]; !ok {
			added = append(added, tag)
		}
	}
	for tag := range currentSet {
		if _, ok := nextSet[tag]; !ok {
			removed = append(removed, tag)
		}
	}
	slices.Sort(added)
	slices.Sort(removed)
	return added, removed
}

// CountAuditActionsByActor returns how many audit events with the given action
// each actor has recorded, keyed by actor_user_id.
func (s *PostgresStore) CountAuditActionsByActor(action string) (map[string]int, error) {
	return s.countByUser(`SELECT actor_user_id, COUNT(*) FROM meme_audit_logs WHERE action = $1 GROUP BY actor_user_id`, action)
}
