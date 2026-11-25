-- Почта пользователей не хранится: убираем колонку и индекс (если они были созданы ранее).
-- Скрипт идемпотентен.
DROP INDEX IF EXISTS idx_users_email_lower;
ALTER TABLE users DROP COLUMN IF EXISTS email;

-- Адрес, записанный в журнал событий при создании администратора командой make admin-create
UPDATE events SET details = details - 'email' WHERE type = 'admin.cli_created';