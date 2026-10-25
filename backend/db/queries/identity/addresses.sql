-- Saved addresses (W2-15; 01 §5.5, C-11, C-18), generated into identitydb.
-- Every query is scoped to the owner: another user's address is simply
-- missing (404). Address text is never logged.

-- name: ListAddresses :many
SELECT id, label, line1, line2, landmark, pincode,
       ST_Y(location::geometry)::float8 AS lat, ST_X(location::geometry)::float8 AS lng,
       is_default, updated_at, version
FROM addresses WHERE user_id = @user_id
ORDER BY is_default DESC, updated_at DESC, id DESC;

-- name: CountAddresses :one
SELECT count(*)::int FROM addresses WHERE user_id = @user_id;

-- name: InsertAddress :one
INSERT INTO addresses (id, user_id, label, line1, line2, landmark, pincode, location, is_default, created_at, updated_at)
VALUES (@id, @user_id, @label, @line1, sqlc.narg(line2), sqlc.narg(landmark), @pincode,
        ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, @is_default, @now, @now)
RETURNING id, label, line1, line2, landmark, pincode,
          ST_Y(location::geometry)::float8 AS lat, ST_X(location::geometry)::float8 AS lng,
          is_default, updated_at, version;

-- name: LockAddress :one
SELECT version, is_default FROM addresses WHERE id = @id AND user_id = @user_id FOR UPDATE;

-- name: ClearOtherDefaults :exec
-- Runs before a row is made the default (ux_addresses_user_default allows one).
UPDATE addresses SET is_default = false, version = version + 1, updated_at = @now
WHERE user_id = @user_id AND is_default AND id <> @id;

-- name: UpdateAddress :one
-- A null narg keeps the value; set_line2 / set_landmark with a null value clear it.
UPDATE addresses SET
  label = coalesce(sqlc.narg(label), label),
  line1 = coalesce(sqlc.narg(line1), line1),
  line2 = CASE WHEN @set_line2::bool THEN sqlc.narg(line2) ELSE line2 END,
  landmark = CASE WHEN @set_landmark::bool THEN sqlc.narg(landmark) ELSE landmark END,
  pincode = coalesce(sqlc.narg(pincode), pincode),
  location = CASE WHEN @set_location::bool
    THEN ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography ELSE location END,
  is_default = coalesce(sqlc.narg(is_default), is_default),
  version = version + 1, updated_at = @now
WHERE id = @id AND user_id = @user_id
RETURNING id, label, line1, line2, landmark, pincode,
          ST_Y(location::geometry)::float8 AS lat, ST_X(location::geometry)::float8 AS lng,
          is_default, updated_at, version;

-- name: DeleteAddress :one
DELETE FROM addresses WHERE id = @id AND user_id = @user_id RETURNING is_default;

-- name: PromoteNewestAddress :exec
-- After the default is deleted, the most recently changed remaining one
-- becomes the default.
UPDATE addresses SET is_default = true, version = version + 1, updated_at = @now
WHERE id = (SELECT a.id FROM addresses a WHERE a.user_id = @user_id ORDER BY a.updated_at DESC, a.id DESC LIMIT 1);
