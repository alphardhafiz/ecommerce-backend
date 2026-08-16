-- orders: transactional data, never deleted, no soft delete (PRD D.2)
CREATE TABLE orders (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users (id),
    status          VARCHAR(20)   NOT NULL DEFAULT 'PENDING',
    total_amount    NUMERIC(12,2) NOT NULL CHECK (total_amount >= 0),
    recipient_name  VARCHAR(100)  NOT NULL,
    phone           VARCHAR(20)   NOT NULL,
    shipping_address TEXT         NOT NULL,
    expired_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX idx_orders_user_created ON orders (user_id, created_at DESC);
CREATE INDEX idx_orders_status ON orders (status);
