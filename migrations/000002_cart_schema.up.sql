CREATE TABLE IF NOT EXISTS carts (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_cart_user ON carts (user_id);
CREATE INDEX IF NOT EXISTS idx_carts_deleted_at ON carts (deleted_at);

CREATE TABLE IF NOT EXISTS cart_lines (
    cart_id TEXT NOT NULL REFERENCES carts (id) ON DELETE CASCADE,
    product_id TEXT NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
    quantity BIGINT NOT NULL,
    PRIMARY KEY (cart_id, product_id),
    CONSTRAINT cart_lines_quantity_check CHECK (quantity > 0)
);

CREATE INDEX IF NOT EXISTS idx_cart_lines_product_id ON cart_lines (product_id);

DO $migration$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'cart_lines'::regclass AND conname = 'cart_lines_quantity_check') THEN
        ALTER TABLE cart_lines ADD CONSTRAINT cart_lines_quantity_check CHECK (quantity > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'carts'::regclass AND conname = 'carts_user_id_fkey') THEN
        ALTER TABLE carts ADD CONSTRAINT carts_user_id_fkey
            FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'cart_lines'::regclass AND conname = 'cart_lines_cart_id_fkey') THEN
        ALTER TABLE cart_lines ADD CONSTRAINT cart_lines_cart_id_fkey
            FOREIGN KEY (cart_id) REFERENCES carts (id) ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'cart_lines'::regclass AND conname = 'cart_lines_product_id_fkey') THEN
        ALTER TABLE cart_lines ADD CONSTRAINT cart_lines_product_id_fkey
            FOREIGN KEY (product_id) REFERENCES products (id) ON DELETE RESTRICT;
    END IF;
END
$migration$;
