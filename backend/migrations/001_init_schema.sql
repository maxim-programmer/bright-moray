CREATE TABLE IF NOT EXISTS dorms (
    id SMALLINT PRIMARY KEY,
    name VARCHAR(100) NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(64) NOT NULL UNIQUE,
    password VARCHAR(255) NOT NULL,
    name VARCHAR(200) NOT NULL,
    telegram VARCHAR(64) NOT NULL UNIQUE,
    dorm_id SMALLINT NOT NULL REFERENCES dorms(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS categories (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    description TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS subcategories (
    id VARCHAR(64) PRIMARY KEY,
    category_id VARCHAR(64) NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    name VARCHAR(200) NOT NULL
);

CREATE TABLE IF NOT EXISTS lots (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(300) NOT NULL,
    description TEXT NOT NULL,
    image VARCHAR(500) NOT NULL,
    category_id VARCHAR(64) NOT NULL REFERENCES categories(id),
    subcategory_id VARCHAR(64) NOT NULL REFERENCES subcategories(id),
    author_id BIGINT NOT NULL REFERENCES users(id),
    start_price INT NOT NULL,
    step INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ends_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'finished', 'cancelled'))
);

CREATE TABLE IF NOT EXISTS bids (
    id BIGSERIAL PRIMARY KEY,
    lot_id BIGINT NOT NULL REFERENCES lots(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id),
    amount INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_lots_category ON lots(category_id);
CREATE INDEX IF NOT EXISTS idx_lots_subcategory ON lots(subcategory_id);
CREATE INDEX IF NOT EXISTS idx_lots_author ON lots(author_id);
CREATE INDEX IF NOT EXISTS idx_lots_status ON lots(status);
CREATE INDEX IF NOT EXISTS idx_lots_ends_at ON lots(ends_at);
CREATE INDEX IF NOT EXISTS idx_lots_start_price ON lots(start_price);
CREATE INDEX IF NOT EXISTS idx_bids_lot ON bids(lot_id);
CREATE INDEX IF NOT EXISTS idx_bids_user ON bids(user_id);
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
CREATE INDEX IF NOT EXISTS idx_users_telegram ON users(telegram);
CREATE INDEX IF NOT EXISTS idx_users_dorm ON users(dorm_id);
