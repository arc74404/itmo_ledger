-- Удаление индексов
DROP INDEX IF EXISTS idx_bonus_entries_user_active_created;
DROP INDEX IF EXISTS idx_bonus_entries_user_id;
DROP INDEX IF EXISTS idx_bonus_entries_expires_at;
DROP INDEX IF EXISTS idx_bonus_entries_user_active;

-- Удаление таблицы
DROP TABLE IF EXISTS bonus_entries;

-- Удаление типа
DROP TYPE IF EXISTS bonus_entry_status;

