ALTER TABLE app_users ADD COLUMN IF NOT EXISTS can_manage_audiobook_series BOOLEAN NOT NULL DEFAULT false;
