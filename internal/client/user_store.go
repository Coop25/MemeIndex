package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"memeindex/internal/accessor"
	"memeindex/internal/dbschema"
)

type managedUserRecord struct {
	UserID         string          `json:"user_id"`
	Username       string          `json:"username"`
	DisplayName    string          `json:"display_name"`
	AvatarURL      string          `json:"avatar_url"`
	LastActiveAt   int64           `json:"last_active_at"`
	Permissions    authPermissions `json:"permissions"`
	SessionVersion int64           `json:"-"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type authUserStore interface {
	BootstrapFromConfig(ctx context.Context, config DiscordAuthConfig) error
	UpsertDiscordProfile(ctx context.Context, user discordUser) error
	UpsertSessionProfile(ctx context.Context, claims authClaims) error
	RecordDeniedVisitor(ctx context.Context, claims authClaims) error
	SessionVersion(ctx context.Context, userID string) (int64, error)
	BumpSessionVersion(ctx context.Context, userID string) error
	GetUser(ctx context.Context, userID string) (managedUserRecord, bool, error)
	ListUsers(ctx context.Context) ([]managedUserRecord, error)
	CreateUser(ctx context.Context, userID string) (managedUserRecord, error)
	UpdateUserPermissions(ctx context.Context, userID string, permissions authPermissions) (managedUserRecord, error)
	// SetUserAudiobookTagRestriction replaces a user's whole audiobook tag
	// allow-list; an empty slice clears it back to unrestricted.
	SetUserAudiobookTagRestriction(ctx context.Context, userID string, tags []string) (managedUserRecord, error)
	DeleteUser(ctx context.Context, userID string) error
}

type postgresAuthUserStore struct {
	pool *pgxpool.Pool
}

func newAuthUserStore(ctx context.Context, databaseURL string) (authUserStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, nil
	}

	pool, err := accessor.NewPool(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect auth user store: %w", err)
	}

	store := &postgresAuthUserStore{pool: pool}
	if err := store.ensureSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (s *postgresAuthUserStore) ensureSchema(ctx context.Context) error {
	if err := dbschema.Apply(ctx, s.pool, "003_app_users_core.sql", "004_app_users_compat.sql", "005_app_user_readd_required.sql", "013_app_user_session_version.sql", "014_app_user_audiobook_permissions.sql", "020_app_user_audiobook_delete_permission.sql", "023_app_user_audiobook_series_permission.sql", "027_app_user_audiobook_tag_restrictions.sql"); err != nil {
		return fmt.Errorf("ensure app_users schema: %w", err)
	}
	return nil
}

func (s *postgresAuthUserStore) BootstrapFromConfig(ctx context.Context, config DiscordAuthConfig) error {
	type seed struct {
		UserID      string
		Permissions authPermissions
	}

	seeds := map[string]seed{}
	for userID := range config.ViewUserIDs {
		normalized := strings.TrimSpace(userID)
		if normalized == "" {
			continue
		}
		if _, isSuperAdmin := config.SuperAdminUserIDs[normalized]; isSuperAdmin {
			continue
		}
		entry := seeds[normalized]
		entry.UserID = normalized
		entry.Permissions.CanView = true
		seeds[normalized] = entry
	}

	for userID := range config.AddUserIDs {
		normalized := strings.TrimSpace(userID)
		if normalized == "" {
			continue
		}
		if _, isSuperAdmin := config.SuperAdminUserIDs[normalized]; isSuperAdmin {
			continue
		}
		entry := seeds[normalized]
		entry.UserID = normalized
		entry.Permissions.CanView = true
		entry.Permissions.CanUpload = true
		seeds[normalized] = entry
	}

	for _, entry := range seeds {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO app_users (
				user_id, can_view, can_upload, can_add_tags, can_remove_tags, can_delete_memes
			)
			SELECT $1, $2, $3, $4, $5, $6
			WHERE NOT EXISTS (
				SELECT 1 FROM app_user_readd_required WHERE user_id = $1
			)
			ON CONFLICT (user_id) DO NOTHING
		`, entry.UserID, entry.Permissions.CanView, entry.Permissions.CanUpload, entry.Permissions.CanAddTags, entry.Permissions.CanRemoveTags, entry.Permissions.CanDeleteMemes); err != nil {
			return fmt.Errorf("seed auth user %s: %w", entry.UserID, err)
		}
	}

	return nil
}

func (s *postgresAuthUserStore) UpsertDiscordProfile(ctx context.Context, user discordUser) error {
	userID := strings.TrimSpace(user.ID)
	if userID == "" {
		return errors.New("user id is required")
	}
	blocked, err := s.isReAddRequired(ctx, userID)
	if err != nil {
		return err
	}
	if blocked {
		return nil
	}

	displayName := strings.TrimSpace(user.GlobalName)
	if displayName == "" {
		displayName = strings.TrimSpace(user.Username)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO app_users (user_id, username, display_name, avatar_url, last_active_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET username = EXCLUDED.username,
			display_name = EXCLUDED.display_name,
			avatar_url = EXCLUDED.avatar_url,
			last_active_at = EXCLUDED.last_active_at,
			updated_at = NOW()
	`, userID, strings.TrimSpace(user.Username), displayName, discordAvatarURL(user), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("upsert discord profile: %w", err)
	}
	return nil
}

func (s *postgresAuthUserStore) UpsertSessionProfile(ctx context.Context, claims authClaims) error {
	userID := strings.TrimSpace(claims.Subject)
	if userID == "" {
		return errors.New("session subject is required")
	}
	blocked, err := s.isReAddRequired(ctx, userID)
	if err != nil {
		return err
	}
	if blocked {
		return nil
	}

	username := strings.TrimSpace(claims.Username)
	displayName := strings.TrimSpace(claims.DisplayName)
	if displayName == "" {
		displayName = username
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO app_users (user_id, username, display_name, avatar_url, last_active_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET username = CASE WHEN EXCLUDED.username <> '' THEN EXCLUDED.username ELSE app_users.username END,
			display_name = CASE WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name ELSE app_users.display_name END,
			avatar_url = CASE WHEN EXCLUDED.avatar_url <> '' THEN EXCLUDED.avatar_url ELSE app_users.avatar_url END,
			last_active_at = EXCLUDED.last_active_at,
			updated_at = NOW()
	`, userID, username, displayName, strings.TrimSpace(claims.AvatarURL), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("upsert session profile: %w", err)
	}
	return nil
}

func (s *postgresAuthUserStore) RecordDeniedVisitor(ctx context.Context, claims authClaims) error {
	userID := strings.TrimSpace(claims.Subject)
	if userID == "" {
		return errors.New("session subject is required")
	}

	username := strings.TrimSpace(claims.Username)
	displayName := strings.TrimSpace(claims.DisplayName)
	if displayName == "" {
		displayName = username
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (user_id, username, display_name, avatar_url, last_active_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET username = CASE WHEN EXCLUDED.username <> '' THEN EXCLUDED.username ELSE app_users.username END,
			display_name = CASE WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name ELSE app_users.display_name END,
			avatar_url = CASE WHEN EXCLUDED.avatar_url <> '' THEN EXCLUDED.avatar_url ELSE app_users.avatar_url END,
			last_active_at = EXCLUDED.last_active_at,
			updated_at = NOW()
	`, userID, username, displayName, strings.TrimSpace(claims.AvatarURL), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("record denied visitor: %w", err)
	}
	return nil
}

func (s *postgresAuthUserStore) isReAddRequired(ctx context.Context, userID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM app_user_readd_required
			WHERE user_id = $1
		)
	`, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check re-add requirement: %w", err)
	}
	return exists, nil
}

// SessionVersion returns the current session version for a user, or 0 when the
// user has no row (deleted or never provisioned). A token whose embedded version
// does not match is treated as revoked by sessionFromRequest.
func (s *postgresAuthUserStore) SessionVersion(ctx context.Context, userID string) (int64, error) {
	normalized := strings.TrimSpace(userID)
	if normalized == "" {
		return 0, errors.New("user id is required")
	}
	var version int64
	err := s.pool.QueryRow(ctx, `
		SELECT session_version FROM app_users WHERE user_id = $1
	`, normalized).Scan(&version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("read session version: %w", err)
	}
	return version, nil
}

// BumpSessionVersion invalidates every session token already issued to a user by
// advancing their session version. It is a no-op when the user has no row.
func (s *postgresAuthUserStore) BumpSessionVersion(ctx context.Context, userID string) error {
	normalized := strings.TrimSpace(userID)
	if normalized == "" {
		return errors.New("user id is required")
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE app_users
		SET session_version = session_version + 1,
			updated_at = NOW()
		WHERE user_id = $1
	`, normalized); err != nil {
		return fmt.Errorf("bump session version: %w", err)
	}
	return nil
}

func (s *postgresAuthUserStore) GetUser(ctx context.Context, userID string) (managedUserRecord, bool, error) {
	record, err := s.getUserByID(ctx, strings.TrimSpace(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return managedUserRecord{}, false, nil
		}
		return managedUserRecord{}, false, err
	}
	return record, true, nil
}

func (s *postgresAuthUserStore) getUserByID(ctx context.Context, userID string) (managedUserRecord, error) {
	var record managedUserRecord
	err := s.pool.QueryRow(ctx, `
		SELECT
			user_id,
			username,
			display_name,
			avatar_url,
			last_active_at,
			can_view,
			can_upload,
			can_add_tags,
			can_remove_tags,
			can_delete_memes,
			can_view_audiobooks,
			can_upload_audiobooks,
			can_delete_audiobooks,
			can_manage_audiobook_series,
			session_version,
			created_at,
			updated_at
		FROM app_users
		WHERE user_id = $1
	`, userID).Scan(
		&record.UserID,
		&record.Username,
		&record.DisplayName,
		&record.AvatarURL,
		&record.LastActiveAt,
		&record.Permissions.CanView,
		&record.Permissions.CanUpload,
		&record.Permissions.CanAddTags,
		&record.Permissions.CanRemoveTags,
		&record.Permissions.CanDeleteMemes,
		&record.Permissions.CanViewAudiobooks,
		&record.Permissions.CanUploadAudiobooks,
		&record.Permissions.CanDeleteAudiobooks,
		&record.Permissions.CanManageAudiobookSeries,
		&record.SessionVersion,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return managedUserRecord{}, err
	}
	if record.Permissions.AudiobookTagRestriction, err = s.tagRestrictionNames(ctx, record.UserID); err != nil {
		return managedUserRecord{}, err
	}
	return record, nil
}

// tagRestrictionNames returns one user's audiobook tag allow-list.
func (s *postgresAuthUserStore) tagRestrictionNames(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.name FROM app_user_audiobook_tag_restrictions r JOIN tags t ON t.id = r.tag_id
		WHERE r.user_id = $1 ORDER BY t.name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list audiobook tag restriction: %w", err)
	}
	defer rows.Close()

	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan audiobook tag restriction: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// allTagRestrictionsByUser returns every user's audiobook tag allow-list in
// one query, keyed by user id - used by ListUsers so the admin page doesn't
// run one extra query per row.
func (s *postgresAuthUserStore) allTagRestrictionsByUser(ctx context.Context) (map[string][]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.user_id, t.name FROM app_user_audiobook_tag_restrictions r JOIN tags t ON t.id = r.tag_id
		ORDER BY r.user_id, t.name
	`)
	if err != nil {
		return nil, fmt.Errorf("list audiobook tag restrictions: %w", err)
	}
	defer rows.Close()

	out := map[string][]string{}
	for rows.Next() {
		var userID, name string
		if err := rows.Scan(&userID, &name); err != nil {
			return nil, fmt.Errorf("scan audiobook tag restriction: %w", err)
		}
		out[userID] = append(out[userID], name)
	}
	return out, rows.Err()
}

func (s *postgresAuthUserStore) ListUsers(ctx context.Context) ([]managedUserRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			user_id,
			username,
			display_name,
			avatar_url,
			last_active_at,
			can_view,
			can_upload,
			can_add_tags,
			can_remove_tags,
			can_delete_memes,
			can_view_audiobooks,
			can_upload_audiobooks,
			can_delete_audiobooks,
			can_manage_audiobook_series,
			session_version,
			created_at,
			updated_at
		FROM app_users
		ORDER BY
			CASE
				WHEN display_name <> '' THEN lower(display_name)
				WHEN username <> '' THEN lower(username)
				ELSE user_id
			END ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	out := []managedUserRecord{}
	for rows.Next() {
		var record managedUserRecord
		if err := rows.Scan(
			&record.UserID,
			&record.Username,
			&record.DisplayName,
			&record.AvatarURL,
			&record.LastActiveAt,
			&record.Permissions.CanView,
			&record.Permissions.CanUpload,
			&record.Permissions.CanAddTags,
			&record.Permissions.CanRemoveTags,
			&record.Permissions.CanDeleteMemes,
			&record.Permissions.CanViewAudiobooks,
			&record.Permissions.CanUploadAudiobooks,
			&record.Permissions.CanDeleteAudiobooks,
			&record.Permissions.CanManageAudiobookSeries,
			&record.SessionVersion,
			&record.CreatedAt,
			&record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}

	restrictions, err := s.allTagRestrictionsByUser(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Permissions.AudiobookTagRestriction = restrictions[out[i].UserID]
	}
	return out, nil
}

func (s *postgresAuthUserStore) CreateUser(ctx context.Context, userID string) (managedUserRecord, error) {
	normalized := strings.TrimSpace(userID)
	if normalized == "" {
		return managedUserRecord{}, errors.New("user id is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return managedUserRecord{}, fmt.Errorf("begin create user: %w", err)
	}
	defer tx.Rollback(ctx)

	// Provision the row before clearing the re-add marker so a resurrected user
	// starts one past the version captured at delete time, invalidating any token
	// that outlived the account.
	_, err = tx.Exec(ctx, `
		INSERT INTO app_users (user_id, session_version, updated_at)
		VALUES ($1, COALESCE((SELECT session_version FROM app_user_readd_required WHERE user_id = $1), 0) + 1, NOW())
		ON CONFLICT (user_id) DO NOTHING
	`, normalized)
	if err != nil {
		return managedUserRecord{}, fmt.Errorf("create user: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM app_user_readd_required
		WHERE user_id = $1
	`, normalized); err != nil {
		return managedUserRecord{}, fmt.Errorf("clear re-add requirement: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return managedUserRecord{}, fmt.Errorf("commit create user: %w", err)
	}

	return s.getUserByID(ctx, normalized)
}

func (s *postgresAuthUserStore) UpdateUserPermissions(ctx context.Context, userID string, permissions authPermissions) (managedUserRecord, error) {
	normalized := strings.TrimSpace(userID)
	if normalized == "" {
		return managedUserRecord{}, errors.New("user id is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return managedUserRecord{}, fmt.Errorf("begin update user permissions: %w", err)
	}
	defer tx.Rollback(ctx)

	// Same ordering as CreateUser: write the row (seeding session_version past the
	// value captured at delete time when this doubles as a re-add) before the
	// re-add marker is cleared. An existing row keeps its current version.
	_, err = tx.Exec(ctx, `
		INSERT INTO app_users (
			user_id, can_view, can_upload, can_add_tags, can_remove_tags, can_delete_memes,
			can_view_audiobooks, can_upload_audiobooks, can_delete_audiobooks, can_manage_audiobook_series, session_version, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE((SELECT session_version FROM app_user_readd_required WHERE user_id = $1), 0) + 1, NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET can_view = EXCLUDED.can_view,
			can_upload = EXCLUDED.can_upload,
			can_add_tags = EXCLUDED.can_add_tags,
			can_remove_tags = EXCLUDED.can_remove_tags,
			can_delete_memes = EXCLUDED.can_delete_memes,
			can_view_audiobooks = EXCLUDED.can_view_audiobooks,
			can_upload_audiobooks = EXCLUDED.can_upload_audiobooks,
			can_delete_audiobooks = EXCLUDED.can_delete_audiobooks,
			can_manage_audiobook_series = EXCLUDED.can_manage_audiobook_series,
			updated_at = NOW()
	`, normalized, permissions.CanView, permissions.CanUpload, permissions.CanAddTags, permissions.CanRemoveTags, permissions.CanDeleteMemes,
		permissions.CanViewAudiobooks, permissions.CanUploadAudiobooks, permissions.CanDeleteAudiobooks, permissions.CanManageAudiobookSeries)
	if err != nil {
		return managedUserRecord{}, fmt.Errorf("update user permissions: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM app_user_readd_required
		WHERE user_id = $1
	`, normalized); err != nil {
		return managedUserRecord{}, fmt.Errorf("clear re-add requirement: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return managedUserRecord{}, fmt.Errorf("commit update user permissions: %w", err)
	}

	return s.getUserByID(ctx, normalized)
}

// SetUserAudiobookTagRestriction overwrites a user's whole audiobook tag
// allow-list, reusing the same shared `tags` table audiobook_tags and
// meme_tags tag into (get-or-create by name, then drop any tag row the
// change leaves with no remaining reference anywhere).
func (s *postgresAuthUserStore) SetUserAudiobookTagRestriction(ctx context.Context, userID string, tags []string) (managedUserRecord, error) {
	normalized := strings.TrimSpace(userID)
	if normalized == "" {
		return managedUserRecord{}, errors.New("user id is required")
	}
	names := normalizeAudiobookTags(tags)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return managedUserRecord{}, fmt.Errorf("begin set audiobook tag restriction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	previousTagIDs, err := scanAudiobookInt64List(ctx, tx, `SELECT tag_id FROM app_user_audiobook_tag_restrictions WHERE user_id = $1`, normalized)
	if err != nil {
		return managedUserRecord{}, fmt.Errorf("list previous audiobook tag restriction ids: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM app_user_audiobook_tag_restrictions WHERE user_id = $1`, normalized); err != nil {
		return managedUserRecord{}, fmt.Errorf("clear audiobook tag restriction: %w", err)
	}

	if len(names) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO tags (name)
			SELECT unnest($1::text[])
			ON CONFLICT (name) DO NOTHING
		`, names); err != nil {
			return managedUserRecord{}, fmt.Errorf("ensure audiobook tag restriction tags exist: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO app_user_audiobook_tag_restrictions (user_id, tag_id)
			SELECT $1, t.id FROM tags t WHERE t.name = ANY($2::text[])
			ON CONFLICT DO NOTHING
		`, normalized, names); err != nil {
			return managedUserRecord{}, fmt.Errorf("set audiobook tag restriction: %w", err)
		}
	}

	if len(previousTagIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM tags
			WHERE id = ANY($1::bigint[])
			  AND NOT EXISTS (SELECT 1 FROM audiobook_tags at WHERE at.tag_id = tags.id)
			  AND NOT EXISTS (SELECT 1 FROM meme_tags mt WHERE mt.tag_id = tags.id)
			  AND NOT EXISTS (SELECT 1 FROM app_user_audiobook_tag_restrictions r WHERE r.tag_id = tags.id)
		`, previousTagIDs); err != nil {
			return managedUserRecord{}, fmt.Errorf("delete orphaned audiobook tag restriction tags: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return managedUserRecord{}, fmt.Errorf("commit set audiobook tag restriction: %w", err)
	}

	return s.getUserByID(ctx, normalized)
}

func (s *postgresAuthUserStore) DeleteUser(ctx context.Context, userID string) error {
	normalized := strings.TrimSpace(userID)
	if normalized == "" {
		return errors.New("user id is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete user: %w", err)
	}
	defer tx.Rollback(ctx)

	// Capture the outgoing session version before the row disappears so a later
	// re-add can resume from a strictly higher number and orphaned tokens issued
	// before the delete never validate again.
	if _, err := tx.Exec(ctx, `
		INSERT INTO app_user_readd_required (user_id, session_version)
		VALUES ($1, COALESCE((SELECT session_version FROM app_users WHERE user_id = $1), 1))
		ON CONFLICT (user_id) DO UPDATE
		SET blocked_at = NOW(),
			session_version = GREATEST(app_user_readd_required.session_version, EXCLUDED.session_version)
	`, normalized); err != nil {
		return fmt.Errorf("mark user for manual re-add: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM app_users
		WHERE user_id = $1
	`, normalized); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete user: %w", err)
	}

	return nil
}
