ALTER TABLE payments
    DROP COLUMN IF EXISTS snap_token,
    DROP COLUMN IF EXISTS redirect_url;