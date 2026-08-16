-- wishlists: unique per user+product to prevent duplicates (PRD D.2)
CREATE TABLE wishlists (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT wishlists_user_product_unique UNIQUE (user_id, product_id)
);

CREATE INDEX idx_wishlists_user_id ON wishlists (user_id);
