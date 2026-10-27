-- Ограничения целостности и индексы. Скрипт идемпотентен.

-- Дубли индексов UNIQUE-колонок и устаревшие индексы
DROP INDEX IF EXISTS idx_users_username;
DROP INDEX IF EXISTS idx_users_telegram;
DROP INDEX IF EXISTS idx_bids_lot;
DROP INDEX IF EXISTS idx_lots_status;

-- Никнейм и Telegram уникальны без учёта регистра
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_username_lower ON users (lower(username));
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_telegram_lower ON users (lower(telegram));

-- Быстрый поиск лидирующей ставки и счётчика ставок
CREATE INDEX IF NOT EXISTS idx_bids_lot_amount ON bids (lot_id, amount DESC, id);
CREATE INDEX IF NOT EXISTS idx_lots_status_ends ON lots (status, ends_at);

-- Подкатегория обязана принадлежать выбранной категории
ALTER TABLE lots DROP CONSTRAINT IF EXISTS lots_subcategory_id_fkey;
ALTER TABLE lots DROP CONSTRAINT IF EXISTS lots_category_sub_fkey;
ALTER TABLE subcategories DROP CONSTRAINT IF EXISTS subcategories_category_sub_key;
ALTER TABLE subcategories ADD CONSTRAINT subcategories_category_sub_key UNIQUE (category_id, id);
ALTER TABLE lots ADD CONSTRAINT lots_category_sub_fkey
    FOREIGN KEY (category_id, subcategory_id) REFERENCES subcategories (category_id, id);

-- Цены и сроки
ALTER TABLE lots DROP CONSTRAINT IF EXISTS lots_prices_chk;
ALTER TABLE lots ADD CONSTRAINT lots_prices_chk CHECK (start_price > 0 AND step > 0);
ALTER TABLE lots DROP CONSTRAINT IF EXISTS lots_period_chk;
ALTER TABLE lots ADD CONSTRAINT lots_period_chk CHECK (ends_at > created_at);
ALTER TABLE bids DROP CONSTRAINT IF EXISTS bids_amount_chk;
ALTER TABLE bids ADD CONSTRAINT bids_amount_chk CHECK (amount > 0);
