DROP INDEX IF EXISTS public.gif_catalog_category_idx;
ALTER TABLE public.gif_catalog DROP COLUMN IF EXISTS category;
