package bots

import (
	"context"
	"strconv"
	"strings"

	"telesrv/internal/domain"
	"telesrv/internal/seed/catalog"
)

// HandlesInlineBot reports whether botUserID is a built-in bot this service
// answers messages.getInlineBotResults for synchronously
// (rpc.ServiceBotInlineResults). Deliberately separate from HandlesBot (see
// that interface's doc comment in internal/rpc/deps.go) -- @gif gets inline
// queries only, not private-message/callback dispatch.
func (s *Service) HandlesInlineBot(botUserID int64) bool {
	return s != nil && botUserID == domain.GifBotUserID
}

// OnInlineQuery serves @gif's admin-curated catalog as inline gif results,
// ordered by title relevance to query (see rankGifCatalogEntries) or, for a
// category-icon tap, filtered to that category (see gifCategoryFromQuery).
//
// offset/paging is not implemented: filtering/ranking runs over the whole
// catalog (an operator's library can run into the thousands, see
// files.Service.ListGifCatalog's doc comment for why that fetch is
// deliberately unbounded), then the result is truncated to
// MaxGifCatalogEntries -- the real per-response cap the client enforces --
// so one response always carries as much of the *relevant* slice as fits,
// not an arbitrary (sort_order, id) prefix of the whole catalog.
//
// Note TDesktop only ever calls this with a non-empty query: its GIF tab has
// no trending panel, and GifsListWidget::searchForGifs returns early on an
// empty string (chat_helpers/gifs_list_widget.cpp), showing saved GIFs alone.
// An empty query is still handled here for clients that do ask for one.
func (s *Service) OnInlineQuery(ctx context.Context, botUserID, _ int64, query, _ string) (domain.BotInlineResults, bool, error) {
	if s == nil || botUserID != domain.GifBotUserID {
		return domain.BotInlineResults{}, false, nil
	}
	if s.gifCatalog == nil {
		return domain.BotInlineResults{Gallery: true}, true, nil
	}
	entries, err := s.gifCatalog.ListGifCatalog(ctx, true)
	if err != nil {
		return domain.BotInlineResults{}, false, err
	}
	if category := gifCategoryFromQuery(query); category != "" {
		// A category-icon tap, not a typed word (see gifCategoryFromQuery) --
		// filter by domain.GifCatalogEntry.Category instead of ranking by
		// title, since the query is an emoji/emoji-blob a title never
		// contains.
		entries = filterGifCatalogEntriesByCategory(entries, category)
	} else {
		entries = rankGifCatalogEntries(entries, query)
	}
	if len(entries) == 0 {
		return domain.BotInlineResults{Gallery: true}, true, nil
	}
	if len(entries) > domain.MaxGifCatalogEntries {
		entries = entries[:domain.MaxGifCatalogEntries]
	}
	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.DocumentID
	}
	docs, err := s.gifCatalog.GetDocuments(ctx, ids)
	if err != nil {
		return domain.BotInlineResults{}, false, err
	}
	byID := make(map[int64]domain.Document, len(docs))
	for _, d := range docs {
		byID[d.ID] = d
	}
	results := make([]domain.BotInlineResult, 0, len(entries))
	for _, e := range entries {
		doc, ok := byID[e.DocumentID]
		if !ok {
			// Catalog entry outlived its document (shouldn't happen -- documents
			// are never deleted -- but skip rather than surface a broken result).
			continue
		}
		docCopy := doc
		results = append(results, domain.BotInlineResult{
			ID:    strconv.FormatInt(e.ID, 10),
			Type:  "gif",
			Title: e.Title,
			Media: &domain.MessageMedia{Kind: domain.MessageMediaKindDocument, Document: &docCopy},
		})
	}
	return domain.BotInlineResults{Gallery: true, Results: results}, true, nil
}

// rankGifCatalogEntries orders title matches first but never drops the rest.
//
// A real @gif searches a huge third-party index, so "no match" there means the
// query genuinely found nothing. A self-hosted catalog is a handful of curated
// files instead: filtering it down to exact title matches would leave the
// picker empty for almost every word an operator's users type, which reads as
// "the feature is broken" rather than "no results". Showing the whole catalog
// with the closest titles first keeps every query useful while still honouring
// the search term. Order within each group stays the admin-set
// (sort_order, id) order the store already applied.
// gifCategoryFromQuery recognizes a GIF-picker category-icon tap and returns
// which domain.GifCatalogCategories entry it names, or "" for an ordinary
// typed search.
//
// The two clients encode a tap completely differently, and neither sends the
// category's name:
//   - Android (StickerCategoriesListView.EmojiCategory.remote, EmojiView.java)
//     concatenates the whole tapped group's emoticons with no separator and
//     sends that as the query -- an exact match against
//     strings.Join(group.Emoticons, "").
//   - TDesktop (GifSectionsValue, stickers_list_footer.cpp) reads a *separate*
//     fixed emoji list (app config's gif_search_emojies, defaulting to 10
//     emoji including some this server never configured) and sends the
//     single tapped emoji as the query -- an exact match against one
//     Emoticons entry.
//
// Both are checked against the same internal/seed/catalog data (the source of
// truth messages.getEmojiGroups itself serves), so no client-side changes or
// server-side emoji-list duplication are needed.
func gifCategoryFromQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	groups, _ := catalog.EmojiGroups()
	for _, g := range groups {
		if len(g.Emoticons) == 0 {
			continue
		}
		if strings.Join(g.Emoticons, "") == query {
			return g.Title
		}
	}
	for _, g := range groups {
		for _, e := range g.Emoticons {
			if e == query {
				return g.Title
			}
		}
	}
	return ""
}

// filterGifCatalogEntriesByCategory keeps only entries tagged with category,
// falling back to the full (enabled) catalog if none are tagged yet -- an
// operator who hasn't categorized anything should still see every GIF on a
// category tap, not an empty picker.
func filterGifCatalogEntriesByCategory(entries []domain.GifCatalogEntry, category string) []domain.GifCatalogEntry {
	filtered := make([]domain.GifCatalogEntry, 0, len(entries))
	for _, e := range entries {
		if e.Category == category {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		return entries
	}
	return filtered
}

func rankGifCatalogEntries(entries []domain.GifCatalogEntry, query string) []domain.GifCatalogEntry {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return entries
	}
	matched := make([]domain.GifCatalogEntry, 0, len(entries))
	rest := make([]domain.GifCatalogEntry, 0, len(entries))
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Title), query) {
			matched = append(matched, e)
		} else {
			rest = append(rest, e)
		}
	}
	return append(matched, rest...)
}
