-- products: soft delete, price is NUMERIC never float (PRD D.2)
CREATE TABLE products (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id UUID REFERENCES categories (id),
    name        VARCHAR(200)  NOT NULL,
    description TEXT,
    price       NUMERIC(12,2) NOT NULL CHECK (price >= 0),
    stock       INT           NOT NULL DEFAULT 0 CHECK (stock >= 0),
    is_active   BOOLEAN       NOT NULL DEFAULT true,
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX idx_products_category_id ON products (category_id);
CREATE INDEX idx_products_active_deleted ON products (is_active, deleted_at);
CREATE INDEX idx_products_name_trgm ON products USING gin (name gin_trgm_ops);
