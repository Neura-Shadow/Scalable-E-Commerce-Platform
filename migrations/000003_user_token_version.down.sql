ALTER TABLE users DROP CONSTRAINT IF EXISTS users_token_version_check;
ALTER TABLE users DROP COLUMN IF EXISTS token_version;
