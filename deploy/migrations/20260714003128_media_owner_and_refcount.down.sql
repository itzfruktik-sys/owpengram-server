DROP INDEX IF EXISTS idx_photos_orphaned_at;
DROP INDEX IF EXISTS idx_documents_orphaned_at;
ALTER TABLE public.photos    DROP COLUMN IF EXISTS orphaned_at;
ALTER TABLE public.documents DROP COLUMN IF EXISTS orphaned_at;

DROP TABLE IF EXISTS public.media_references;

DROP INDEX IF EXISTS idx_photos_owner_user_id;
DROP INDEX IF EXISTS idx_documents_owner_user_id;
ALTER TABLE public.photos    DROP COLUMN IF EXISTS owner_user_id;
ALTER TABLE public.documents DROP COLUMN IF EXISTS owner_user_id;
