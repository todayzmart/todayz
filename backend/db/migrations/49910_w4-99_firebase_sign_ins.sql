-- D-275: Firebase Auth sends and checks the sign-in SMS code; the API checks
-- the Firebase ID token and opens the TodayZ session itself. Each phone
-- verification signs in once: firebase_sign_ins holds one row per verification
-- (the keyed hash of the Firebase uid and auth_time, never the token, the uid
-- or the number), so a second exchange of the same token, or of a refreshed
-- token from the same verification, is refused with 401 otp_expired.
--
-- A row is needed only while a token for its verification is still accepted
-- (auth_time within 5 minutes, FirebaseMaxAuthAge); expires_at is kept a little
-- past that and identity_purge deletes rows once it has passed. Times are
-- written from the application clock, never a now() default.

-- +goose Up
CREATE TABLE firebase_sign_ins (
  key_hash   bytea PRIMARY KEY,
  used_at    timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  CONSTRAINT ck_firebase_sign_ins_expires CHECK (expires_at > used_at)
);
CREATE INDEX ix_firebase_sign_ins_expires ON firebase_sign_ins (expires_at);

-- +goose Down
DROP TABLE firebase_sign_ins;
