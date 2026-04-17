package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"memeindex/internal/dbschema"
)

type managedUserRecord struct {
	UserID       string          `json:"user_id"`
	Username     string          `json:"username"`
	DisplayName  string          `json:"display_name"`
	AvatarURL    string          `json:"avatar_url"`
	LastActiveAt int64           `json:"last_active_at"`
	Permissions  authPermissions `json:"permissions"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type authUserStore interface {
	BootstrapFromConfig(ctx context.Context, config DiscordAuthConfig) error
	UpsertDiscordProfile(ctx context.Context, user discordUser) error
	UpsertSessionProfile(ctx context.Context, claims authClaims) error
	GetUser(ctx context.Context, userID string) (managedUserRecord, bool, error)
	ListUsers(ctx context.Context) ([]managedUserRecord, error)
	CreateUser(ctx context.Context, userID string) (managedUserRecord, error)
	UpdateUserPermissions(ctx context.Context, userID string, permissions authPermissions) (managedUserRecord, error)
}

type postgresAuthUserStore struct {
	pool *pgxpool.Pool
}

func newAuthUserStore(ctx context.Context, databaseURL string) (authUserStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, nil
	}

	pool, err := pgxpool.New(ctx, databaseURL)
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
	if err := dbschema.Apply(ctx, s.pool, "003_app_users_core.sql", "004_app_users_compat.sql"); err != nil {
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
			) VALUES ($1, $2, $3, $4, $5, $6)
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

	displayName := strings.TrimSpace(user.GlobalName)
	if displayName == "" {
		displayName = strings.TrimSpace(user.Username)
	}

	_, err := s.pool.Exec(ctx, `
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
		return fmt.Errorf("upsert session profile: %w", err)
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
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return managedUserRecord{}, err
	}
	return record, nil
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
	return out, nil
}

func (s *postgresAuthUserStore) CreateUser(ctx context.Context, userID string) (managedUserRecord, error) {
	normalized := strings.TrimSpace(userID)
	if normalized == "" {
		return managedUserRecord{}, errors.New("user id is required")
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (user_id, updated_at)
		VALUES ($1, NOW())
		ON CONFLICT (user_id) DO NOTHING
	`, normalized)
	if err != nil {
		return managedUserRecord{}, fmt.Errorf("create user: %w", err)
	}

	return s.getUserByID(ctx, normalized)
}

func (s *postgresAuthUserStore) UpdateUserPermissions(ctx context.Context, userID string, permissions authPermissions) (managedUserRecord, error) {
	normalized := strings.TrimSpace(userID)
	if normalized == "" {
		return managedUserRecord{}, errors.New("user id is required")
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_users (
			user_id, can_view, can_upload, can_add_tags, can_remove_tags, can_delete_memes, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET can_view = EXCLUDED.can_view,
			can_upload = EXCLUDED.can_upload,
			can_add_tags = EXCLUDED.can_add_tags,
			can_remove_tags = EXCLUDED.can_remove_tags,
			can_delete_memes = EXCLUDED.can_delete_memes,
			updated_at = NOW()
	`, normalized, permissions.CanView, permissions.CanUpload, permissions.CanAddTags, permissions.CanRemoveTags, permissions.CanDeleteMemes)
	if err != nil {
		return managedUserRecord{}, fmt.Errorf("update user permissions: %w", err)
	}

	return s.getUserByID(ctx, normalized)
}
