UPDATE public.users
SET verified = false,
    updated_at = now()
WHERE id = 1250000013;
