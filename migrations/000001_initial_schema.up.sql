-- Validate legacy GORM-managed data before this migration creates any index or
-- constraint. Operators can repair the named invariant and safely rerun v1
-- after resetting only the dirty migration metadata.
DO $preflight$
BEGIN
    IF to_regclass('public.users') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM users WHERE role IS NOT NULL AND role NOT IN ('admin', 'customer')) THEN
            RAISE EXCEPTION 'cannot adopt users because role contains unsupported values';
        END IF;
        IF EXISTS (
            SELECT 1 FROM users WHERE email IS NOT NULL GROUP BY email HAVING count(*) > 1
        ) THEN
            RAISE EXCEPTION 'cannot adopt users because email contains duplicate values';
        END IF;
    END IF;

    IF to_regclass('public.products') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM products WHERE price IS NOT NULL AND price <= 0) THEN
            RAISE EXCEPTION 'cannot adopt products because price must be greater than zero';
        END IF;
        IF EXISTS (
            SELECT 1 FROM products WHERE code IS NOT NULL GROUP BY code HAVING count(*) > 1
        ) THEN
            RAISE EXCEPTION 'cannot adopt products because code contains duplicate values';
        END IF;
        IF EXISTS (
            SELECT 1 FROM products WHERE name IS NOT NULL GROUP BY name HAVING count(*) > 1
        ) THEN
            RAISE EXCEPTION 'cannot adopt products because name contains duplicate values';
        END IF;
    END IF;

    IF to_regclass('public.inventories') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM inventories WHERE quantity IS NOT NULL AND quantity < 0) THEN
            RAISE EXCEPTION 'cannot adopt inventories because quantity contains negative values';
        END IF;
        IF EXISTS (
            SELECT 1 FROM inventories WHERE product_id IS NOT NULL GROUP BY product_id HAVING count(*) > 1
        ) THEN
            RAISE EXCEPTION 'cannot adopt inventories because product_id contains duplicate values';
        END IF;
        IF to_regclass('public.products') IS NOT NULL AND EXISTS (
            SELECT 1
            FROM inventories i
            LEFT JOIN products p ON p.id = i.product_id
            WHERE i.product_id IS NOT NULL AND p.id IS NULL
        ) THEN
            RAISE EXCEPTION 'cannot adopt inventories because product_id contains orphaned values';
        END IF;
    END IF;

    IF to_regclass('public.orders') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM orders WHERE total_price IS NOT NULL AND total_price < 0) THEN
            RAISE EXCEPTION 'cannot adopt orders because total_price contains negative values';
        END IF;
        IF EXISTS (
            SELECT 1 FROM orders
            WHERE status IS NOT NULL AND status NOT IN ('new', 'in-progress', 'done', 'cancelled')
        ) THEN
            RAISE EXCEPTION 'cannot adopt orders because status contains unsupported values';
        END IF;
        IF EXISTS (
            SELECT 1 FROM orders WHERE code IS NOT NULL GROUP BY code HAVING count(*) > 1
        ) THEN
            RAISE EXCEPTION 'cannot adopt orders because code contains duplicate values';
        END IF;
        IF to_regclass('public.users') IS NOT NULL AND EXISTS (
            SELECT 1
            FROM orders o
            LEFT JOIN users u ON u.id = o.user_id
            WHERE o.user_id IS NOT NULL AND u.id IS NULL
        ) THEN
            RAISE EXCEPTION 'cannot adopt orders because user_id contains orphaned values';
        END IF;
    END IF;

    IF to_regclass('public.order_lines') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM order_lines WHERE quantity IS NOT NULL AND quantity <= 0) THEN
            RAISE EXCEPTION 'cannot adopt order_lines because quantity must be greater than zero';
        END IF;
        IF EXISTS (SELECT 1 FROM order_lines WHERE price IS NOT NULL AND price < 0) THEN
            RAISE EXCEPTION 'cannot adopt order_lines because price contains negative values';
        END IF;
        IF to_regclass('public.orders') IS NOT NULL AND EXISTS (
            SELECT 1
            FROM order_lines ol
            LEFT JOIN orders o ON o.id = ol.order_id
            WHERE ol.order_id IS NOT NULL AND o.id IS NULL
        ) THEN
            RAISE EXCEPTION 'cannot adopt order_lines because order_id contains orphaned values';
        END IF;
        IF to_regclass('public.products') IS NOT NULL AND EXISTS (
            SELECT 1
            FROM order_lines ol
            LEFT JOIN products p ON p.id = ol.product_id
            WHERE ol.product_id IS NOT NULL AND p.id IS NULL
        ) THEN
            RAISE EXCEPTION 'cannot adopt order_lines because product_id contains orphaned values';
        END IF;
    END IF;

    IF to_regclass('public.outbox_events') IS NOT NULL THEN
        IF EXISTS (
            SELECT 1 FROM outbox_events
            WHERE status IS NOT NULL AND status NOT IN ('pending', 'processing', 'published', 'dead_letter')
        ) THEN
            RAISE EXCEPTION 'cannot adopt outbox_events because status contains unsupported values';
        END IF;
        IF EXISTS (SELECT 1 FROM outbox_events WHERE attempts IS NOT NULL AND attempts < 0) THEN
            RAISE EXCEPTION 'cannot adopt outbox_events because attempts contains negative values';
        END IF;
    END IF;
END
$preflight$;

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    email TEXT NOT NULL UNIQUE,
    password TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'customer',
    CONSTRAINT users_role_check CHECK (role IN ('admin', 'customer'))
);

CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users (deleted_at);
CREATE INDEX IF NOT EXISTS idx_user_email ON users (email);

CREATE TABLE IF NOT EXISTS products (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    price NUMERIC NOT NULL,
    active BOOLEAN NOT NULL DEFAULT true,
    CONSTRAINT products_price_check CHECK (price > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_product_code ON products (code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_product_name ON products (name);
CREATE INDEX IF NOT EXISTS idx_products_deleted_at ON products (deleted_at);

CREATE TABLE IF NOT EXISTS inventories (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    product_id TEXT NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
    quantity BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT inventories_quantity_check CHECK (quantity >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_inventory_product ON inventories (product_id);
CREATE INDEX IF NOT EXISTS idx_inventories_deleted_at ON inventories (deleted_at);

CREATE TABLE IF NOT EXISTS orders (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    code TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    total_price NUMERIC NOT NULL,
    status TEXT NOT NULL DEFAULT 'new',
    CONSTRAINT orders_total_price_check CHECK (total_price >= 0),
    CONSTRAINT orders_status_check CHECK (status IN ('new', 'in-progress', 'done', 'cancelled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_code ON orders (code);
CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders (user_id);
CREATE INDEX IF NOT EXISTS idx_orders_user_status_created_at ON orders (user_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_deleted_at ON orders (deleted_at);

CREATE TABLE IF NOT EXISTS order_lines (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    order_id TEXT NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    product_id TEXT NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
    quantity BIGINT NOT NULL,
    price NUMERIC NOT NULL,
    CONSTRAINT order_lines_quantity_check CHECK (quantity > 0),
    CONSTRAINT order_lines_price_check CHECK (price >= 0)
);

CREATE INDEX IF NOT EXISTS idx_order_lines_order_id ON order_lines (order_id);
CREATE INDEX IF NOT EXISTS idx_order_lines_product_id ON order_lines (product_id);
CREATE INDEX IF NOT EXISTS idx_order_lines_deleted_at ON order_lines (deleted_at);

CREATE TABLE IF NOT EXISTS outbox_events (
    id TEXT PRIMARY KEY,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts BIGINT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    locked_by VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    CONSTRAINT outbox_events_status_check
        CHECK (status IN ('pending', 'processing', 'published', 'dead_letter')),
    CONSTRAINT outbox_events_attempts_check CHECK (attempts >= 0)
);

ALTER TABLE outbox_events ADD COLUMN IF NOT EXISTS locked_at TIMESTAMPTZ;
ALTER TABLE outbox_events ADD COLUMN IF NOT EXISTS locked_by VARCHAR(128);

CREATE INDEX IF NOT EXISTS idx_outbox_events_status_next_attempt_at
    ON outbox_events (status, next_attempt_at)
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_outbox_events_processing_locked_at
    ON outbox_events (locked_at)
    WHERE status = 'processing';
CREATE INDEX IF NOT EXISTS idx_outbox_aggregate ON outbox_events (aggregate_type, aggregate_id);
CREATE INDEX IF NOT EXISTS idx_outbox_events_event_type ON outbox_events (event_type);
CREATE INDEX IF NOT EXISTS idx_outbox_events_created_at ON outbox_events (created_at);
CREATE INDEX IF NOT EXISTS idx_outbox_events_published_at
    ON outbox_events (published_at)
    WHERE published_at IS NOT NULL;

DO $migration$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'users'::regclass AND conname = 'users_role_check') THEN
        ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('admin', 'customer'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'products'::regclass AND conname = 'products_price_check') THEN
        ALTER TABLE products ADD CONSTRAINT products_price_check CHECK (price > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'inventories'::regclass AND conname = 'inventories_quantity_check') THEN
        ALTER TABLE inventories ADD CONSTRAINT inventories_quantity_check CHECK (quantity >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'orders'::regclass AND conname = 'orders_total_price_check') THEN
        ALTER TABLE orders ADD CONSTRAINT orders_total_price_check CHECK (total_price >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'orders'::regclass AND conname = 'orders_status_check') THEN
        ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN ('new', 'in-progress', 'done', 'cancelled'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'order_lines'::regclass AND conname = 'order_lines_quantity_check') THEN
        ALTER TABLE order_lines ADD CONSTRAINT order_lines_quantity_check CHECK (quantity > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'order_lines'::regclass AND conname = 'order_lines_price_check') THEN
        ALTER TABLE order_lines ADD CONSTRAINT order_lines_price_check CHECK (price >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'outbox_events'::regclass AND conname = 'outbox_events_status_check') THEN
        ALTER TABLE outbox_events ADD CONSTRAINT outbox_events_status_check
            CHECK (status IN ('pending', 'processing', 'published', 'dead_letter'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'outbox_events'::regclass AND conname = 'outbox_events_attempts_check') THEN
        ALTER TABLE outbox_events ADD CONSTRAINT outbox_events_attempts_check CHECK (attempts >= 0);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'inventories'::regclass AND conname = 'inventories_product_id_fkey') THEN
        ALTER TABLE inventories ADD CONSTRAINT inventories_product_id_fkey
            FOREIGN KEY (product_id) REFERENCES products (id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'orders'::regclass AND conname = 'orders_user_id_fkey') THEN
        ALTER TABLE orders ADD CONSTRAINT orders_user_id_fkey
            FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'order_lines'::regclass AND conname = 'order_lines_order_id_fkey') THEN
        ALTER TABLE order_lines ADD CONSTRAINT order_lines_order_id_fkey
            FOREIGN KEY (order_id) REFERENCES orders (id) ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'order_lines'::regclass AND conname = 'order_lines_product_id_fkey') THEN
        ALTER TABLE order_lines ADD CONSTRAINT order_lines_product_id_fkey
            FOREIGN KEY (product_id) REFERENCES products (id) ON DELETE RESTRICT;
    END IF;
END
$migration$;
