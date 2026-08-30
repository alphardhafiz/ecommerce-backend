-- persist the Midtrans Snap token/redirect so GET /orders/:id can offer
-- "pay now" for PENDING orders without re-creating a transaction
ALTER TABLE payments
    ADD COLUMN snap_token  VARCHAR(255),
    ADD COLUMN redirect_url TEXT;