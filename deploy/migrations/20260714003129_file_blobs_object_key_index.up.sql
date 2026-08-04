-- Supports the reference-count check ("is any other row still using this
-- object_key on this backend?") that runs before a blob is physically
-- deleted from its backend during storage retention sweeps.

CREATE INDEX idx_file_blobs_object_key_backend ON public.file_blobs (backend, object_key);
