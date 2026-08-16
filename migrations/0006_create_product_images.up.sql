-- product_images: no soft delete, child of products (PRD D.2)
CREATE TABLE product_images (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id    UUID NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    url           VARCHAR(500) NOT NULL,
    is_primary    BOOLEAN      NOT NULL DEFAULT false,
    display_order INT          NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_product_images_product_id ON product_images (product_id);
