-- addresses: single-default enforced in service layer, not DB (PRD D.2)
CREATE TABLE addresses (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    label          VARCHAR(50),
    recipient_name VARCHAR(100) NOT NULL,
    phone          VARCHAR(20)  NOT NULL,
    full_address   TEXT         NOT NULL,
    city           VARCHAR(100) NOT NULL,
    province       VARCHAR(100) NOT NULL,
    postal_code    VARCHAR(10)  NOT NULL,
    is_default     BOOLEAN      NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_addresses_user_id ON addresses (user_id);
