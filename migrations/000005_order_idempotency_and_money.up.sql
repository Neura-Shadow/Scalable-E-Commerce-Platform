-- Monetary values accepted by the API use two decimal minor units. Refuse to
-- silently round legacy values before installing the database constraints.
DO $preflight$
BEGIN
    IF EXISTS (SELECT 1 FROM products WHERE price IS NOT NULL AND price <> round(price, 2)) THEN
        RAISE EXCEPTION 'cannot enforce minor-unit prices because products.price contains more than two decimals';
    END IF;
    IF EXISTS (SELECT 1 FROM orders WHERE total_price IS NOT NULL AND total_price <> round(total_price, 2)) THEN
        RAISE EXCEPTION 'cannot enforce minor-unit prices because orders.total_price contains more than two decimals';
    END IF;
    IF EXISTS (SELECT 1 FROM order_lines WHERE price IS NOT NULL AND price <> round(price, 2)) THEN
        RAISE EXCEPTION 'cannot enforce minor-unit prices because order_lines.price contains more than two decimals';
    END IF;
END
$preflight$;

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(64),
    ADD COLUMN IF NOT EXISTS idempotency_fingerprint VARCHAR(64);

DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'orders'::regclass AND conname = 'orders_idempotency_pair_check'
    ) THEN
        ALTER TABLE orders ADD CONSTRAINT orders_idempotency_pair_check CHECK (
            (idempotency_key IS NULL AND idempotency_fingerprint IS NULL)
            OR (idempotency_key IS NOT NULL AND idempotency_fingerprint IS NOT NULL)
        );
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'orders'::regclass AND conname = 'orders_idempotency_key_length_check'
    ) THEN
        ALTER TABLE orders ADD CONSTRAINT orders_idempotency_key_length_check CHECK (
            idempotency_key IS NULL OR length(idempotency_key) = 64
        );
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'orders'::regclass AND conname = 'orders_idempotency_fingerprint_length_check'
    ) THEN
        ALTER TABLE orders ADD CONSTRAINT orders_idempotency_fingerprint_length_check CHECK (
            idempotency_fingerprint IS NULL OR length(idempotency_fingerprint) = 64
        );
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'products'::regclass AND conname = 'products_price_scale_check'
    ) THEN
        ALTER TABLE products ADD CONSTRAINT products_price_scale_check CHECK (price = round(price, 2));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'orders'::regclass AND conname = 'orders_total_price_scale_check'
    ) THEN
        ALTER TABLE orders ADD CONSTRAINT orders_total_price_scale_check CHECK (total_price = round(total_price, 2));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'order_lines'::regclass AND conname = 'order_lines_price_scale_check'
    ) THEN
        ALTER TABLE order_lines ADD CONSTRAINT order_lines_price_scale_check CHECK (price = round(price, 2));
    END IF;
END
$migration$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_user_idempotency_key
    ON orders (user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
