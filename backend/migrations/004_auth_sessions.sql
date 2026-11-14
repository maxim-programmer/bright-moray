-- Сессии пользователей: refresh-токены с ротацией и обнаружением повторного использования.
-- Каждая авторизация создаёт «семейство» (family_id); каждое обновление токена выпускает
-- нового члена семейства, а предыдущий помечается использованным (used_at, replaced_by).
-- Предъявление уже использованного токена означает кражу — отзывается всё семейство.
-- Сам токен в БД не хранится, только SHA-256 хеш (32 байта).

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id         UUID        NOT NULL,
    token_hash        BYTEA       NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    -- срок бездействия: created_at + 7 суток, но не позже family_expires_at
    expires_at        TIMESTAMPTZ NOT NULL,
    -- абсолютный предел всей сессии (30 суток с момента входа), одинаков для семейства
    family_expires_at TIMESTAMPTZ NOT NULL,
    used_at           TIMESTAMPTZ,
    replaced_by       BIGINT REFERENCES refresh_tokens(id) ON DELETE SET NULL,
    revoked_at        TIMESTAMPTZ,
    revoked_reason    TEXT,

    CONSTRAINT refresh_tokens_hash_uq UNIQUE (token_hash),
    CONSTRAINT refresh_tokens_hash_len_chk CHECK (octet_length(token_hash) = 32),
    CONSTRAINT refresh_tokens_period_chk CHECK (expires_at > created_at AND expires_at <= family_expires_at),
    CONSTRAINT refresh_tokens_replaced_chk CHECK (replaced_by IS NULL OR used_at IS NOT NULL),
    CONSTRAINT refresh_tokens_revoked_chk CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL)),
    CONSTRAINT refresh_tokens_reason_chk CHECK (revoked_reason IS NULL OR revoked_reason IN
        ('logout', 'logout_all', 'password_change', 'reuse_detected', 'relogin'))
);

-- Отзыв всех сессий пользователя и поиск его активных токенов
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_active ON refresh_tokens (user_id)
    WHERE revoked_at IS NULL AND used_at IS NULL;
-- Отзыв семейства
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_family ON refresh_tokens (family_id);
-- Очистка истории
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_family_expires ON refresh_tokens (family_expires_at);
-- Внешний ключ replaced_by (быстрое удаление цепочек)
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_replaced_by ON refresh_tokens (replaced_by)
    WHERE replaced_by IS NOT NULL;
