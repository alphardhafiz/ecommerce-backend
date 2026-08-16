-- order_items: product_name/price are snapshots, never joined live (PRD D.2)
CREATE TABLE order_items (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id     UUID NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    product_id   UUID NOT NULL REFERENCES products (id),
    product_name VARCHAR(200)  NOT NULL,
    price        NUMERIC(12,2) NOT NULL,
    quantity     INT           NOT NULL CHECK (quantity > 0),
    subtotal     NUMERIC(12,2) NOT NULL,
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX idx_order_items_order_id ON order_items (order_id);
