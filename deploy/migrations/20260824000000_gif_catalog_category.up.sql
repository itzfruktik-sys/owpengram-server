-- Tags a gif_catalog entry with one of the messages.getEmojiGroups category
-- titles (see internal/seed/catalog/emoji_groups.json) so tapping a category
-- icon in the client's GIF picker can filter to that emotion instead of
-- always showing the whole catalog (see files.ClassifyGifCategory). Empty
-- means uncategorized -- still served by plain text search, just not by any
-- category tap.
ALTER TABLE public.gif_catalog ADD COLUMN category text DEFAULT ''::text NOT NULL;

CREATE INDEX gif_catalog_category_idx ON public.gif_catalog (category) WHERE category <> '';
