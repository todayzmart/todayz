-- testkit's copy of the local database role contract (lead note to W0-03,
-- decided with W0-02; the local stack's copy is infra/local/postgres/init.sql).
-- backend/ must not import infra/, so this file mirrors it. Keep them in step.
--
-- Cluster level, run once per container as the superuser.
--   todayz_owner  owns the schema; migrations run as it
--   todayz_app    api, worker and seed; DML only, through default privileges
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'todayz_owner') THEN
    CREATE ROLE todayz_owner LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'todayz_app') THEN
    CREATE ROLE todayz_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
  END IF;
END
$$;
