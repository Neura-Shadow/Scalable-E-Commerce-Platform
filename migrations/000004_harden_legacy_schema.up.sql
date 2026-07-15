-- Converge databases previously created by GORM AutoMigrate with the explicit schema.
-- Safe defaults are backfilled automatically. Ambiguous business data must be repaired
-- by an operator before this migration can proceed.

DO $migration$
DECLARE
    target RECORD;
    has_null BOOLEAN;
BEGIN
    FOR target IN
        SELECT *
        FROM (VALUES
            ('users', 'id'),
            ('users', 'email'),
            ('users', 'password'),
            ('products', 'id'),
            ('products', 'code'),
            ('products', 'name'),
            ('products', 'description'),
            ('products', 'price'),
            ('inventories', 'id'),
            ('inventories', 'product_id'),
            ('orders', 'id'),
            ('orders', 'code'),
            ('orders', 'user_id'),
            ('orders', 'total_price'),
            ('order_lines', 'id'),
            ('order_lines', 'order_id'),
            ('order_lines', 'product_id'),
            ('order_lines', 'quantity'),
            ('order_lines', 'price'),
            ('outbox_events', 'id'),
            ('outbox_events', 'aggregate_type'),
            ('outbox_events', 'aggregate_id'),
            ('outbox_events', 'event_type'),
            ('outbox_events', 'payload'),
            ('carts', 'id'),
            ('carts', 'user_id'),
            ('cart_lines', 'cart_id'),
            ('cart_lines', 'product_id'),
            ('cart_lines', 'quantity')
        ) AS ambiguous_columns(table_name, column_name)
    LOOP
        EXECUTE format(
            'SELECT EXISTS (SELECT 1 FROM %I WHERE %I IS NULL)',
            target.table_name,
            target.column_name
        ) INTO has_null;

        IF has_null THEN
            RAISE EXCEPTION 'cannot harden %.% because NULL values require manual remediation',
                target.table_name,
                target.column_name;
        END IF;
    END LOOP;
END
$migration$;

UPDATE users
SET created_at = COALESCE(created_at, now()),
    updated_at = COALESCE(updated_at, now()),
    role = COALESCE(role, 'customer'),
    token_version = COALESCE(token_version, 0)
WHERE created_at IS NULL
   OR updated_at IS NULL
   OR role IS NULL
   OR token_version IS NULL;

UPDATE products
SET created_at = COALESCE(created_at, now()),
    updated_at = COALESCE(updated_at, now()),
    active = COALESCE(active, true)
WHERE created_at IS NULL
   OR updated_at IS NULL
   OR active IS NULL;

UPDATE inventories
SET created_at = COALESCE(created_at, now()),
    updated_at = COALESCE(updated_at, now()),
    quantity = COALESCE(quantity, 0)
WHERE created_at IS NULL
   OR updated_at IS NULL
   OR quantity IS NULL;

UPDATE orders
SET created_at = COALESCE(created_at, now()),
    updated_at = COALESCE(updated_at, now()),
    status = COALESCE(status, 'new')
WHERE created_at IS NULL
   OR updated_at IS NULL
   OR status IS NULL;

UPDATE order_lines
SET created_at = COALESCE(created_at, now()),
    updated_at = COALESCE(updated_at, now())
WHERE created_at IS NULL
   OR updated_at IS NULL;

UPDATE outbox_events
SET status = COALESCE(status, 'pending'),
    attempts = COALESCE(attempts, 0),
    next_attempt_at = COALESCE(next_attempt_at, now()),
    created_at = COALESCE(created_at, now())
WHERE status IS NULL
   OR attempts IS NULL
   OR next_attempt_at IS NULL
   OR created_at IS NULL;

UPDATE carts
SET created_at = COALESCE(created_at, now()),
    updated_at = COALESCE(updated_at, now())
WHERE created_at IS NULL
   OR updated_at IS NULL;

DO $migration$
DECLARE
    target RECORD;
    has_null BOOLEAN;
BEGIN
    FOR target IN
        SELECT *
        FROM (VALUES
            ('users', 'id'),
            ('users', 'created_at'),
            ('users', 'updated_at'),
            ('users', 'email'),
            ('users', 'password'),
            ('users', 'role'),
            ('users', 'token_version'),
            ('products', 'id'),
            ('products', 'created_at'),
            ('products', 'updated_at'),
            ('products', 'code'),
            ('products', 'name'),
            ('products', 'description'),
            ('products', 'price'),
            ('products', 'active'),
            ('inventories', 'id'),
            ('inventories', 'created_at'),
            ('inventories', 'updated_at'),
            ('inventories', 'product_id'),
            ('inventories', 'quantity'),
            ('orders', 'id'),
            ('orders', 'created_at'),
            ('orders', 'updated_at'),
            ('orders', 'code'),
            ('orders', 'user_id'),
            ('orders', 'total_price'),
            ('orders', 'status'),
            ('order_lines', 'id'),
            ('order_lines', 'created_at'),
            ('order_lines', 'updated_at'),
            ('order_lines', 'order_id'),
            ('order_lines', 'product_id'),
            ('order_lines', 'quantity'),
            ('order_lines', 'price'),
            ('outbox_events', 'id'),
            ('outbox_events', 'aggregate_type'),
            ('outbox_events', 'aggregate_id'),
            ('outbox_events', 'event_type'),
            ('outbox_events', 'payload'),
            ('outbox_events', 'status'),
            ('outbox_events', 'attempts'),
            ('outbox_events', 'next_attempt_at'),
            ('outbox_events', 'created_at'),
            ('carts', 'id'),
            ('carts', 'created_at'),
            ('carts', 'updated_at'),
            ('carts', 'user_id'),
            ('cart_lines', 'cart_id'),
            ('cart_lines', 'product_id'),
            ('cart_lines', 'quantity')
        ) AS required_columns(table_name, column_name)
    LOOP
        EXECUTE format(
            'SELECT EXISTS (SELECT 1 FROM %I WHERE %I IS NULL)',
            target.table_name,
            target.column_name
        ) INTO has_null;

        IF has_null THEN
            RAISE EXCEPTION 'cannot harden %.% because NULL values require manual remediation',
                target.table_name,
                target.column_name;
        END IF;
    END LOOP;
END
$migration$;

ALTER TABLE users
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL,
    ALTER COLUMN email SET NOT NULL,
    ALTER COLUMN password SET NOT NULL,
    ALTER COLUMN role SET DEFAULT 'customer',
    ALTER COLUMN role SET NOT NULL,
    ALTER COLUMN token_version SET DEFAULT 0,
    ALTER COLUMN token_version SET NOT NULL;

ALTER TABLE products
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL,
    ALTER COLUMN code SET NOT NULL,
    ALTER COLUMN name SET NOT NULL,
    ALTER COLUMN description SET NOT NULL,
    ALTER COLUMN price SET NOT NULL,
    ALTER COLUMN active SET DEFAULT true,
    ALTER COLUMN active SET NOT NULL;

ALTER TABLE inventories
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL,
    ALTER COLUMN product_id SET NOT NULL,
    ALTER COLUMN quantity SET DEFAULT 0,
    ALTER COLUMN quantity SET NOT NULL;

ALTER TABLE orders
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL,
    ALTER COLUMN code SET NOT NULL,
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN total_price SET NOT NULL,
    ALTER COLUMN status SET DEFAULT 'new',
    ALTER COLUMN status SET NOT NULL;

ALTER TABLE order_lines
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL,
    ALTER COLUMN order_id SET NOT NULL,
    ALTER COLUMN product_id SET NOT NULL,
    ALTER COLUMN quantity SET NOT NULL,
    ALTER COLUMN price SET NOT NULL;

ALTER TABLE outbox_events
    ALTER COLUMN aggregate_type SET NOT NULL,
    ALTER COLUMN aggregate_id SET NOT NULL,
    ALTER COLUMN event_type SET NOT NULL,
    ALTER COLUMN payload SET NOT NULL,
    ALTER COLUMN status SET DEFAULT 'pending',
    ALTER COLUMN status SET NOT NULL,
    ALTER COLUMN attempts SET DEFAULT 0,
    ALTER COLUMN attempts SET NOT NULL,
    ALTER COLUMN next_attempt_at SET DEFAULT now(),
    ALTER COLUMN next_attempt_at SET NOT NULL,
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN created_at SET NOT NULL;

ALTER TABLE carts
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL,
    ALTER COLUMN user_id SET NOT NULL;

ALTER TABLE cart_lines
    ALTER COLUMN cart_id SET NOT NULL,
    ALTER COLUMN product_id SET NOT NULL,
    ALTER COLUMN quantity SET NOT NULL;
