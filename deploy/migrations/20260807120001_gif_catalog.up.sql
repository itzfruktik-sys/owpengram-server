-- Admin-curated GIF catalog: what @gif (see 20260807120000) serves as inline
-- results for the client's GIF picker. document_id/document_ids are loose
-- bigint references (no FK), matching every other media table in this schema
-- (e.g. sticker_sets.thumb_document_id) -- documents are immutable once
-- created, so there is nothing to cascade.
CREATE TABLE public.gif_catalog (
    id bigint PRIMARY KEY,
    title text DEFAULT ''::text NOT NULL,
    document_id bigint NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    created_by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX gif_catalog_enabled_sort_idx ON public.gif_catalog (enabled, sort_order, id);
