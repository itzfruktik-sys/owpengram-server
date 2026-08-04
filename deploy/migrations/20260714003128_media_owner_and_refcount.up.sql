-- Storage management: track who uploaded each document/photo, and track
-- every live reference to it (message boxes, channel messages, profile
-- photos, sticker material, gifts) so orphaned media (nothing left pointing
-- at it) can be identified safely for later cleanup. A media row's
-- orphaned_at is set the instant its last reference is removed, and cleared
-- if a new reference appears -- retention sweeps only ever act on rows where
-- orphaned_at is set, so media still visible in a live conversation is never
-- touched regardless of age.

ALTER TABLE public.documents ADD COLUMN owner_user_id bigint DEFAULT 0 NOT NULL;
ALTER TABLE public.photos    ADD COLUMN owner_user_id bigint DEFAULT 0 NOT NULL;
-- Pre-existing rows predate ownership tracking and have no recorded
-- uploader; they stay at 0 ("unattributed") rather than being backfilled
-- with a guess, and are surfaced as their own bucket in the admin UI.

CREATE INDEX idx_documents_owner_user_id ON public.documents (owner_user_id) WHERE owner_user_id <> 0;
CREATE INDEX idx_photos_owner_user_id    ON public.photos    (owner_user_id) WHERE owner_user_id <> 0;

CREATE TABLE public.media_references (
    media_kind text NOT NULL,
    media_id   bigint NOT NULL,
    ref_kind   text NOT NULL,
    ref_key    text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (media_kind, media_id, ref_kind, ref_key)
);
CREATE INDEX idx_media_references_media ON public.media_references (media_kind, media_id);

ALTER TABLE public.documents ADD COLUMN orphaned_at timestamp with time zone;
ALTER TABLE public.photos    ADD COLUMN orphaned_at timestamp with time zone;
CREATE INDEX idx_documents_orphaned_at ON public.documents (orphaned_at) WHERE orphaned_at IS NOT NULL;
CREATE INDEX idx_photos_orphaned_at    ON public.photos    (orphaned_at) WHERE orphaned_at IS NOT NULL;
