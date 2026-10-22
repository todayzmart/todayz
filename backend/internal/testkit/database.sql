-- Per database, run as the superuser in a database owned by todayz_owner,
-- before any migration (see roles.sql). Extensions are the superuser's job;
-- the baseline migration only checks that they exist. The list matches
-- infra/local/postgres/init.sql: citext is not here because it is a trusted
-- extension the owner creates in the baseline migration (migrate TestExtensions proves it).
-- The extension and privilege statements below match init.sql's per-database
-- block exactly (TestMatchesLocalInitSQL).
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

-- No temporary tables for anyone but the owner: a temporary table named like
-- a catalog or app table is how a session shadows unqualified names. A
-- database's ACL is not copied by CREATE DATABASE … TEMPLATE, so testkit also
-- runs this REVOKE for every copy (createPrepared in testkit.go).
DO $$
BEGIN
  EXECUTE format('REVOKE TEMPORARY ON DATABASE %I FROM PUBLIC', current_database());
END
$$;
