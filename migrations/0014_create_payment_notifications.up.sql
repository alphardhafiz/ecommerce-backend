-- payment_notifications: audit log for every webhook delivery (PRD D.2)
CREATE TABLE payment_notifications (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id         UUID NOT NULL REFERENCES payments (id),
    raw_payload        JSONB        NOT NULL,
    transaction_status VARCHAR(30)  NOT NULL,
    received_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_payment_notifications_payment_received ON payment_notifications (payment_id, received_at);
