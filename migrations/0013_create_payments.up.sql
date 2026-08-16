-- payments: one order = one payment (PRD D.2)
CREATE TABLE payments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id          UUID NOT NULL UNIQUE REFERENCES orders (id),
    midtrans_order_id VARCHAR(100) NOT NULL,
    status            VARCHAR(20)   NOT NULL DEFAULT 'PENDING',
    amount            NUMERIC(12,2) NOT NULL,
    payment_type      VARCHAR(50),
    paid_at           TIMESTAMPTZ,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT payments_midtrans_order_id_unique UNIQUE (midtrans_order_id)
);
