-- Renames the built-in third-party-verification bot from @verifierbot
-- ("Verifier Bot") to @marksbot ("Marks Bot").
--
-- The old name was too easy to confuse at a glance with @verifybot ("Verify
-- Bot"), the unrelated official-checkmark front door (0153/0112) -- one grants
-- the platform badge, the other grants a third-party icon+description mark,
-- and the two must never read each other's state (see internal/domain/system.go
-- and 0156/20260714003115). This migration only touches the identity (name,
-- handle); the account id, access_hash and its bot_verifier_settings grant (if
-- any operator already made one) are untouched.
--
-- 20260714003115 already applied on any existing deployment created the
-- account under the old name via INSERT ... ON CONFLICT DO UPDATE -- editing
-- that historical file in place would not reach a database where it already
-- ran, hence a separate migration here instead.

UPDATE public.users
SET first_name = 'Marks Bot',
    username = 'marksbot',
    updated_at = now()
WHERE id = 1250000013;

UPDATE public.peer_usernames
SET username_lower = 'marksbot',
    username = 'marksbot',
    updated_at = now()
WHERE peer_type = 'user' AND peer_id = 1250000013 AND username_lower = 'verifierbot';
