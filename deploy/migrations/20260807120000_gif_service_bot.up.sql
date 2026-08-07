-- Built-in @gif: answers messages.getInlineBotResults synchronously (server-side,
-- no MTProto session or Bot API process -- see rpc.ServiceBotInlineResults) with
-- the admin-curated GIF catalog, the same role Telegram's own @gif plays for the
-- client's GIF picker "trending"/search panel.
--
-- Seeded here rather than lazily on first message so the username is occupied
-- from the moment the schema is current, same rationale as 20260714003112's
-- @verifybot seed.
--
-- access_hash is double-written with domain.GifBotAccessHash; the two must never
-- drift, exactly as for the other service bots.
--
-- inline_placeholder must be non-empty: internal/rpc/bots_inline.go's
-- onMessagesGetInlineBotResults refuses BOT_INLINE_DISABLED otherwise.

INSERT INTO public.users (
    id, access_hash, phone, first_name, last_name, username, country_code,
    created_at, updated_at, verified, support, about, last_seen_at,
    default_history_ttl_period, is_bot, bot_info_version, premium_expires_at,
    emoji_status_document_id, emoji_status_until, color_set, color,
    color_background_emoji_id, profile_color_set, profile_color,
    profile_color_background_emoji_id
) VALUES (
    1250000015, 7233282977235616768, '', 'GIFs', '', 'gif', '',
    now(), now(), true, false,
    'Search and browse this server''s GIF catalog.',
    0, 0, true, 1, NULL, 0, 0, false, 0, 0, false, 0, 0
)
ON CONFLICT (id) DO UPDATE SET
    access_hash = EXCLUDED.access_hash,
    phone = EXCLUDED.phone,
    first_name = EXCLUDED.first_name,
    last_name = EXCLUDED.last_name,
    username = EXCLUDED.username,
    verified = EXCLUDED.verified,
    support = EXCLUDED.support,
    about = EXCLUDED.about,
    is_bot = EXCLUDED.is_bot,
    bot_info_version = GREATEST(public.users.bot_info_version, EXCLUDED.bot_info_version),
    updated_at = now();

INSERT INTO public.bots (
    bot_user_id, owner_user_id, token_secret, description, commands,
    bot_chat_history, bot_nochats, inline_placeholder, created_at, updated_at,
    menu_button_type, menu_button_text, menu_button_url, bot_inline_geo
) VALUES (
    1250000015, 1250000015, '',
    'Search and browse this server''s GIF catalog from the GIF picker.',
    '[]'::jsonb,
    false, true, 'Search GIFs', now(), now(), 0, '', '', false
)
ON CONFLICT (bot_user_id) DO UPDATE SET
    owner_user_id = EXCLUDED.owner_user_id,
    token_secret = EXCLUDED.token_secret,
    description = EXCLUDED.description,
    commands = EXCLUDED.commands,
    bot_chat_history = EXCLUDED.bot_chat_history,
    bot_nochats = EXCLUDED.bot_nochats,
    inline_placeholder = EXCLUDED.inline_placeholder,
    menu_button_type = EXCLUDED.menu_button_type,
    menu_button_text = EXCLUDED.menu_button_text,
    menu_button_url = EXCLUDED.menu_button_url,
    bot_inline_geo = EXCLUDED.bot_inline_geo,
    updated_at = now();

INSERT INTO public.peer_usernames (
    username_lower, username, peer_type, peer_id, active, editable, sort_order, updated_at
)
VALUES ('gif', 'gif', 'user', 1250000015, true, true, 0, now())
ON CONFLICT (username_lower) DO UPDATE SET
    username = EXCLUDED.username,
    peer_type = EXCLUDED.peer_type,
    peer_id = EXCLUDED.peer_id,
    active = EXCLUDED.active,
    editable = EXCLUDED.editable,
    updated_at = now();

INSERT INTO public.read_model_versions (model, owner_user_id, peer_type, peer_id, version, updated_at, hash)
VALUES
    ('contact_account', 1250000015, 'user', 1250000015, 1, now(), 12500000150001),
    ('channel_active_memberships', 1250000015, 'user', 1250000015, 1, now(), 12500000150002)
ON CONFLICT (model, owner_user_id, peer_type, peer_id) DO UPDATE SET
    version = GREATEST(public.read_model_versions.version, EXCLUDED.version),
    updated_at = now(),
    hash = EXCLUDED.hash;
