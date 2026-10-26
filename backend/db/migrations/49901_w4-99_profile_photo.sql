-- The customer's profile photo (D-253, founder 26 Oct: it shows at once, with no admin review, and
-- only the customer sees it). The app registers an upload and PUTs the file into the private
-- bucket; the confirm step reads it back, re-encodes it (dropping EXIF and every other piece of
-- metadata) to profile-photos/<userId>/<uploadId>.jpg and points users.photo_key at that copy.
-- The photo is served only through a presigned GET on the customer's own GET /v1/me.
--
-- user_photo_uploads keeps every registration: the rolling-hour limit counts them, an upload left
-- unconfirmed past its URL is cleaned up ('expired') at the customer's next registration, and
-- account deletion removes every object they name.

-- +goose Up
ALTER TABLE users
  ADD COLUMN photo_key text,
  ADD CONSTRAINT ck_users_photo_key CHECK (photo_key IS NULL OR char_length(photo_key) BETWEEN 1 AND 300);

CREATE TABLE user_photo_uploads (
  id           uuid PRIMARY KEY,
  user_id      uuid NOT NULL REFERENCES users (id),
  object_key   text NOT NULL,
  content_type text NOT NULL,
  size_bytes   int NOT NULL,
  status       text NOT NULL DEFAULT 'uploading',
  created_at   timestamptz NOT NULL DEFAULT now(),
  confirmed_at timestamptz,
  CONSTRAINT ck_user_photo_uploads_object_key CHECK (char_length(object_key) BETWEEN 1 AND 300),
  CONSTRAINT ck_user_photo_uploads_content_type CHECK (content_type IN ('image/jpeg','image/png','image/webp')),
  CONSTRAINT ck_user_photo_uploads_size CHECK (size_bytes > 0),
  CONSTRAINT ck_user_photo_uploads_status CHECK (status IN ('uploading','done','expired')),
  CONSTRAINT ck_user_photo_uploads_confirmed CHECK ((status = 'done') = (confirmed_at IS NOT NULL))
);
CREATE INDEX ix_user_photo_uploads_user_created ON user_photo_uploads (user_id, created_at DESC);

-- 01 §17 (D-253). TestSettingsMatchProductRules reads this insert (D-95).
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('profile_photo_uploads_per_hour', '10', 'count', 1, 60, 'Account. 01 §2: profile photo uploads one customer may ask for in a rolling hour');

-- +goose Down
DELETE FROM platform_settings WHERE key = 'profile_photo_uploads_per_hour';
DROP TABLE user_photo_uploads;
ALTER TABLE users
  DROP CONSTRAINT ck_users_photo_key,
  DROP COLUMN photo_key;
