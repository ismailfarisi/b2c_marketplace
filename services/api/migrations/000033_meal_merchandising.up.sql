-- Meals merchandising: staff decide what the storefront home shows.
--
-- A layout is an ordered list of sections stored on an immutable version.
-- Editing happens on the single draft; publishing turns the draft into a
-- version that goes live now or at publish_at. The live version is the
-- newest published one whose time has come, so a scheduled publish needs no
-- worker, and rolling back is publishing a copy of an older version.
CREATE TABLE merch_collections (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    title       text NOT NULL,
    title_ar    text,
    subtitle    text,
    subtitle_ar text,
    -- {cuisines, dietary, slots, rotation_only, max_price_minor}; every list
    -- is "any of", and an empty rule set matches every kitchen.
    rules       jsonb NOT NULL DEFAULT '{}',
    sort        text NOT NULL DEFAULT 'rating' CHECK (sort IN ('rating', 'price_asc', 'price_desc', 'newest')),
    max_items   int CHECK (max_items BETWEEN 1 AND 48),
    starts_at   timestamptz,
    ends_at     timestamptz,
    is_active   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at)
);
CREATE UNIQUE INDEX merch_collections_slug_key ON merch_collections (slug) WHERE deleted_at IS NULL;

-- Pins are shown first in position order even when the rules would not pick
-- them; excludes are removed even when they would.
CREATE TABLE merch_collection_items (
    collection_id uuid NOT NULL REFERENCES merch_collections (id) ON DELETE CASCADE,
    branch_id     uuid NOT NULL REFERENCES vendor_branches (id),
    role          text NOT NULL CHECK (role IN ('pin', 'exclude')),
    position      int NOT NULL DEFAULT 0,
    PRIMARY KEY (collection_id, branch_id)
);

CREATE TABLE merch_banners (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title      text NOT NULL,
    title_ar   text,
    body       text,
    body_ar    text,
    cta        text,
    cta_ar     text,
    image_url  text,
    link_url   text,
    starts_at  timestamptz,
    ends_at    timestamptz,
    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at)
);

CREATE TABLE merch_layouts (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug       text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE merch_layout_versions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    layout_id      uuid NOT NULL REFERENCES merch_layouts (id) ON DELETE CASCADE,
    version_number int NOT NULL,
    status         text NOT NULL CHECK (status IN ('draft', 'published')),
    sections       jsonb NOT NULL DEFAULT '[]',
    publish_at     timestamptz,
    published_by   uuid,
    note           text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (layout_id, version_number),
    CHECK (status = 'draft' OR publish_at IS NOT NULL)
);
CREATE UNIQUE INDEX merch_layout_versions_one_draft ON merch_layout_versions (layout_id) WHERE status = 'draft';
CREATE INDEX merch_layout_versions_live_idx ON merch_layout_versions (layout_id, publish_at DESC) WHERE status = 'published';

INSERT INTO merch_layouts (slug) VALUES ('meals_home');
