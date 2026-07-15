DROP INDEX IF EXISTS idx_orders_user_idempotency_key;

ALTER TABLE orders
    DROP CONSTRAINT IF EXISTS orders_idempotency_pair_check,
    DROP CONSTRAINT IF EXISTS orders_idempotency_key_length_check,
    DROP CONSTRAINT IF EXISTS orders_idempotency_fingerprint_length_check,
    DROP CONSTRAINT IF EXISTS orders_total_price_scale_check,
    DROP COLUMN IF EXISTS idempotency_fingerprint,
    DROP COLUMN IF EXISTS idempotency_key;

ALTER TABLE order_lines
    DROP CONSTRAINT IF EXISTS order_lines_price_scale_check;

ALTER TABLE products
    DROP CONSTRAINT IF EXISTS products_price_scale_check;
