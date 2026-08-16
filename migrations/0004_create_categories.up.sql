CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- categories: soft delete, never hard-deleted (PRD D.2)
CREATE TABLE categories (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(100) NOT NULL,
    slug       VARCHAR(120) NOT NULL,
    is_active  BOOLEAN      NOT NULL DEFAULT true,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT categories_slug_unique UNIQUE (slug)
);

CREATE INDEX idx_categories_active ON categories (slug) WHERE deleted_at IS NULL AND is_active = true;
