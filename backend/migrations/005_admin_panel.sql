-- Панель администратора: роли, блокировка, модерация лотов, журнал событий, системные настройки.
-- Скрипт идемпотентен.

-- ---------- Роли и блокировка пользователей ----------

ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(16) NOT NULL DEFAULT 'user';
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_chk;
ALTER TABLE users ADD CONSTRAINT users_role_chk CHECK (role IN ('user', 'admin'));

ALTER TABLE users ADD COLUMN IF NOT EXISTS blocked_at     TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS blocked_reason TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS blocked_by     BIGINT REFERENCES users(id) ON DELETE SET NULL;

-- Почта необязательна (при регистрации не спрашивается); задаётся, например, командой make admin-create.
ALTER TABLE users ADD COLUMN IF NOT EXISTS email VARCHAR(254);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_lower ON users (lower(email)) WHERE email IS NOT NULL;

-- Администратор, созданный через терминал, не проходит регистрацию:
-- у него может не быть Telegram и общежития (справочник общежитий ведёт сам администратор).
ALTER TABLE users ALTER COLUMN dorm_id DROP NOT NULL;
ALTER TABLE users ALTER COLUMN telegram DROP NOT NULL;

-- Отзыв сессий при блокировке
ALTER TABLE refresh_tokens DROP CONSTRAINT IF EXISTS refresh_tokens_reason_chk;
ALTER TABLE refresh_tokens ADD CONSTRAINT refresh_tokens_reason_chk CHECK (revoked_reason IS NULL OR revoked_reason IN
    ('logout', 'logout_all', 'password_change', 'reuse_detected', 'relogin', 'blocked'));

-- ---------- Модерация лотов ----------
-- pending   — ждёт решения администратора
-- approved  — одобрен, ждёт начала аукциона (starts_at в будущем)
-- rejected  — отклонён (причина в moderation_reason)
-- active    — идут торги
-- finished  — аукцион завершён
-- cancelled — аукцион отменён автором или администратором

ALTER TABLE lots DROP CONSTRAINT IF EXISTS lots_status_check;
ALTER TABLE lots ADD CONSTRAINT lots_status_check
    CHECK (status IN ('pending', 'approved', 'rejected', 'active', 'finished', 'cancelled'));

-- Длительность хранится отдельно: пока лот не начался, ends_at — лишь расчётное значение,
-- а при старте аукциона оно пересчитывается как «начало + длительность».
ALTER TABLE lots ADD COLUMN IF NOT EXISTS duration_seconds BIGINT;
ALTER TABLE lots ADD COLUMN IF NOT EXISTS starts_at        TIMESTAMPTZ;
ALTER TABLE lots ADD COLUMN IF NOT EXISTS cancelled_at     TIMESTAMPTZ;
ALTER TABLE lots ADD COLUMN IF NOT EXISTS moderation_reason TEXT;
ALTER TABLE lots ADD COLUMN IF NOT EXISTS moderated_by     BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE lots ADD COLUMN IF NOT EXISTS moderated_at     TIMESTAMPTZ;
-- hidden = лот снят с публикации администратором: его видят только автор и администраторы
ALTER TABLE lots ADD COLUMN IF NOT EXISTS hidden           BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE lots DROP CONSTRAINT IF EXISTS lots_duration_chk;
ALTER TABLE lots ADD CONSTRAINT lots_duration_chk CHECK (duration_seconds IS NULL OR duration_seconds > 0);

-- Уже существующие лоты были опубликованы сразу
UPDATE lots SET duration_seconds = GREATEST(1, EXTRACT(EPOCH FROM (ends_at - created_at))::BIGINT)
    WHERE duration_seconds IS NULL;
UPDATE lots SET starts_at = created_at
    WHERE starts_at IS NULL AND status IN ('active', 'finished', 'cancelled');
UPDATE lots SET cancelled_at = created_at
    WHERE cancelled_at IS NULL AND status = 'cancelled';

CREATE INDEX IF NOT EXISTS idx_lots_approved_start ON lots (starts_at) WHERE status = 'approved';
CREATE INDEX IF NOT EXISTS idx_lots_pending ON lots (created_at) WHERE status = 'pending';

-- ---------- Журнал событий ----------
-- Записи не привязаны к лоту внешним ключом: история лота должна пережить его удаление.

CREATE TABLE IF NOT EXISTS events (
    id         BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    type       VARCHAR(64) NOT NULL,
    -- о каком пользователе событие (кто зарегистрировался, кто сделал ставку, кого заблокировали)
    user_id    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    -- кто совершил действие (для действий администратора — он сам)
    actor_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    by_admin   BOOLEAN NOT NULL DEFAULT false,
    lot_id     BIGINT,
    details    JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_events_created ON events (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_events_user ON events (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_actor ON events (actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_lot ON events (lot_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_type ON events (type, created_at DESC);

-- ---------- Системные настройки ----------
-- Хранятся только значения, изменённые администратором; для остальных действуют значения по умолчанию.

CREATE TABLE IF NOT EXISTS settings (
    key        VARCHAR(64) PRIMARY KEY,
    value      BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL
);
