-- Down migrations exist for local development only. Production rollback is a
-- backup restore plus compatible code, or a forward-fix migration.
DROP TRIGGER IF EXISTS trg_tasks_updated_at ON tasks;
DROP FUNCTION IF EXISTS set_updated_at();
DROP TABLE IF EXISTS tasks;
