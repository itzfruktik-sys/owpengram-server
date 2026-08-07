DROP INDEX IF EXISTS public.gif_catalog_source_filename_uniq;
ALTER TABLE public.gif_catalog DROP COLUMN IF EXISTS source_filename;
