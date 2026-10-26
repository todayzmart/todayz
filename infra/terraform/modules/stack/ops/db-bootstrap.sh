#!/bin/sh
# Prod database bootstrap (D-254): the deployed twin of infra/local/postgres/init.sql. Runs once
# (and again harmlessly) as the RDS master user in the ops db-bootstrap task, which ECS starts
# with these variables from Secrets Manager: PGUSER and PGPASSWORD (the RDS-managed master
# secret), DATABASE_URL (todayz_app) and DATABASE_MIGRATE_URL (todayz_owner). The role
# passwords are taken from those URLs, so the URLs in Secrets Manager stay the only copy.
# Nothing is echoed: psql runs without -a/-e and the passwords travel as psql variables.
set -eu

pw_from_url() {
  # postgres://user:password@host:port/db?... -> password (scripts/deploy/secrets.sh writes hex)
  printf '%s' "$1" | sed -E 's#^[a-z]+://[^:/@]+:([^@]+)@.*$#\1#'
}

APP_PW="$(pw_from_url "$DATABASE_URL")"
OWNER_PW="$(pw_from_url "$DATABASE_MIGRATE_URL")"
case "$APP_PW$OWNER_PW" in
  *[!0-9a-f]*) echo "db-bootstrap: the database URLs must carry hex passwords (scripts/deploy/secrets.sh database)" >&2; exit 1 ;;
esac
[ -n "$APP_PW" ] && [ -n "$OWNER_PW" ] || { echo "db-bootstrap: a database URL has no password" >&2; exit 1; }

psql -X -q -v ON_ERROR_STOP=1 -v app_pw="$APP_PW" -v owner_pw="$OWNER_PW" -d "${PGDATABASE:-todayz}" <<'SQL'
SELECT 'CREATE ROLE todayz_owner LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS'
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'todayz_owner')\gexec
SELECT 'CREATE ROLE todayz_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS'
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'todayz_app')\gexec
ALTER ROLE todayz_owner WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD :'owner_pw';
ALTER ROLE todayz_app WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD :'app_pw';

-- The master is rds_superuser, not a superuser: on PG16 it must be a member of a role to hand
-- it ownership.
GRANT todayz_owner TO CURRENT_USER;
ALTER DATABASE todayz OWNER TO todayz_owner;
REVOKE TEMPORARY ON DATABASE todayz FROM PUBLIC;

CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
ALTER SCHEMA public OWNER TO todayz_owner;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO todayz_app;
ALTER DEFAULT PRIVILEGES FOR ROLE todayz_owner IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO todayz_app;
ALTER DEFAULT PRIVILEGES FOR ROLE todayz_owner IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO todayz_app;
ALTER DEFAULT PRIVILEGES FOR ROLE todayz_owner IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO todayz_app;
ALTER DEFAULT PRIVILEGES FOR ROLE todayz_owner REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
SQL
echo "db-bootstrap: roles, extensions and privileges in place"
