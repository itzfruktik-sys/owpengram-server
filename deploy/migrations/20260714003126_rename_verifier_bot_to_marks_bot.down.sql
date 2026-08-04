UPDATE public.peer_usernames
SET username_lower = 'verifierbot',
    username = 'verifierbot',
    updated_at = now()
WHERE peer_type = 'user' AND peer_id = 1250000013 AND username_lower = 'marksbot';

UPDATE public.users
SET first_name = 'Verifier Bot',
    username = 'verifierbot',
    updated_at = now()
WHERE id = 1250000013;
