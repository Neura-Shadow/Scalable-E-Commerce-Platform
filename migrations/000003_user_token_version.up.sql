ALTER TABLE users
    ADD COLUMN IF NOT EXISTS token_version BIGINT NOT NULL DEFAULT 0;

DO $migration$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'users'::regclass AND conname = 'users_token_version_check') THEN
        ALTER TABLE users ADD CONSTRAINT users_token_version_check CHECK (token_version >= 0);
    END IF;
END
$migration$;
