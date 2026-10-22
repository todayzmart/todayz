-- TodayZ local Postgres bootstrap (W0-02). Idempotent: it runs once from
-- docker-entrypoint-initdb.d on a fresh volume, and again on every `make dev`
-- and `make reset` (psql -d postgres -f), so an old volume converges too.
--
-- Roles (passwords are local-only, non-secret values; nothing here reaches staging or prod):
--   todayz           superuser (compose POSTGRES_USER). make reset and admin only; no app uses it.
--   todayz_owner     owns the databases and schema public; migrations run as it (DATABASE_MIGRATE_URL).
--   todayz_app       the API and workers (DATABASE_URL): read/write on todayz_owner's tables,
--                    no DDL. Not a superuser.
--   grafana_ro       Grafana datasource: read-only.
--   todayz_exporter  postgres_exporter: statistics only.
-- Extensions are created here as superuser, in both app databases, because a
-- non-superuser owner cannot create most of them. Migrations may repeat
-- CREATE EXTENSION IF NOT EXISTS; it is a no-op.

\set ON_ERROR_STOP on
\connect postgres

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'todayz_owner') THEN
    CREATE ROLE todayz_owner LOGIN PASSWORD 'todayz_owner' NOSUPERUSER NOCREATEDB NOCREATEROLE;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'todayz_app') THEN
    CREATE ROLE todayz_app LOGIN PASSWORD 'todayz_app' NOSUPERUSER NOCREATEDB NOCREATEROLE;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'todayz_exporter') THEN
    CREATE ROLE todayz_exporter LOGIN PASSWORD 'todayz_exporter' NOSUPERUSER NOCREATEDB NOCREATEROLE;
  END IF;
  -- pg_read_all_data is cluster-wide, so it survives make reset dropping and recreating databases.
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ro') THEN
    CREATE ROLE grafana_ro LOGIN PASSWORD 'grafana_ro' NOSUPERUSER NOCREATEDB NOCREATEROLE;
  END IF;
END
$$;
-- Converge roles made by an older init.sql.
ALTER ROLE todayz_owner NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
ALTER ROLE todayz_app NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
GRANT pg_monitor TO todayz_exporter;
GRANT pg_read_all_data TO grafana_ro;
GRANT pg_monitor TO grafana_ro;
ALTER ROLE grafana_ro SET default_transaction_read_only = on;

-- App databases (todayz for make dev, todayz_test for tests that want a fixed DB).
SELECT 'CREATE DATABASE todayz OWNER todayz_owner'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'todayz')\gexec
SELECT 'CREATE DATABASE todayz_test OWNER todayz_owner'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'todayz_test')\gexec
ALTER DATABASE todayz OWNER TO todayz_owner;
ALTER DATABASE todayz_test OWNER TO todayz_owner;

-- pg_stat_statements is preloaded by the server command line; the copy in the
-- maintenance DB is what postgres_exporter reads.
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- Per database: extensions, schema ownership and the app's privileges.
-- (psql variables cannot loop, so the block is repeated for each database.)
\connect todayz
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

\connect todayz_test
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
