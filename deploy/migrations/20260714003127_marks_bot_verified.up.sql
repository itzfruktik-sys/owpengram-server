-- @marksbot (formerly @verifierbot) now carries the platform checkmark itself
-- (domain.VerifierBotUser().Verified = true). This is a deliberate operator
-- choice on top of the original upstream design (which kept it unverified to
-- avoid implying the third-party mark it grants is somehow platform-endorsed):
-- the checkmark here only asserts "this account is a legitimate first-party
-- service bot", not "this bot's grants are official" -- the two mechanisms
-- remain fully independent regardless of this flag.

UPDATE public.users
SET verified = true,
    updated_at = now()
WHERE id = 1250000013;
