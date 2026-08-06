-- Admin-triggered broadcast messages sent from the official system account
-- (777000) to all users or a hand-picked list. Delivery is a durable outbox
-- (broadcast_recipients) drained by a periodic worker, mirroring the
-- verification_notification_outbox pattern: the admin action only has to
-- snapshot the recipient list and return, not send potentially thousands of
-- messages inline within one HTTP request.

CREATE TABLE public.broadcasts (
    id bigserial PRIMARY KEY,
    message text NOT NULL,
    target_mode character varying(16) NOT NULL,
    total_count integer NOT NULL DEFAULT 0,
    created_by character varying(64) NOT NULL DEFAULT '',
    created_at timestamp with time zone NOT NULL DEFAULT now()
);

CREATE TABLE public.broadcast_recipients (
    id bigserial PRIMARY KEY,
    broadcast_id bigint NOT NULL REFERENCES public.broadcasts(id) ON DELETE CASCADE,
    user_id bigint NOT NULL,
    status character varying(16) NOT NULL DEFAULT 'pending',
    attempts integer NOT NULL DEFAULT 0,
    last_error text NOT NULL DEFAULT '',
    sent_at timestamp with time zone,
    UNIQUE (broadcast_id, user_id)
);

-- The worker's whole read pattern is "give me pending rows, oldest first";
-- a partial index keeps that cheap forever regardless of how many rows have
-- already settled into sent/failed.
CREATE INDEX idx_broadcast_recipients_pending ON public.broadcast_recipients (id) WHERE status = 'pending';
CREATE INDEX idx_broadcast_recipients_broadcast ON public.broadcast_recipients (broadcast_id);
