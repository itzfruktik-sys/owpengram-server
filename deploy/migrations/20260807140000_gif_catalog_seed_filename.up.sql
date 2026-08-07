-- Tracks which gif_catalog entries came from a filesystem seed drop
-- (data/gifs/, see files.Service.SeedGifs) vs. an admin-panel upload, so a
-- restart can tell "already imported this file" from "brand new file" without
-- re-transcoding every GIF on every startup. Empty for panel-created entries.
ALTER TABLE public.gif_catalog ADD COLUMN source_filename text DEFAULT ''::text NOT NULL;

-- Only seed-imported rows need uniqueness here: two panel uploads may
-- legitimately share nothing to compare, but two seed imports of the same
-- filename must collapse to one row on every restart.
CREATE UNIQUE INDEX gif_catalog_source_filename_uniq
    ON public.gif_catalog (source_filename)
    WHERE source_filename <> '';
