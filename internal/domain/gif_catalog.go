package domain

import (
	"errors"
	"time"
)

var (
	// ErrGifCatalogUnavailable is returned by the admin GIF-catalog write path
	// when no store.GifCatalogStore was configured (files.WithGifCatalog).
	ErrGifCatalogUnavailable = errors.New("gif catalog is not configured")
	// ErrGifCatalogFileInvalid is returned when an uploaded file isn't a GIF
	// or MP4, or exceeds MaxGifCatalogUploadSize.
	ErrGifCatalogFileInvalid = errors.New("gif catalog file invalid")
	// ErrGifCatalogEntryInvalid is returned for a title that fails validation
	// or a document_id that doesn't resolve to an uploaded document.
	ErrGifCatalogEntryInvalid = errors.New("gif catalog entry invalid")
	// ErrGifCatalogEntryNotFound is returned by an update/delete against an id
	// that doesn't exist.
	ErrGifCatalogEntryNotFound = errors.New("gif catalog entry not found")
)

const (
	// MaxGifCatalogTitleLen bounds an admin-entered catalog entry title.
	MaxGifCatalogTitleLen = 128
	// MaxGifCatalogEntries caps how many entries @gif serves in one inline
	// response -- mirrors MaxBotInlineResults, the TL-level cap the client
	// itself enforces per messages.getInlineBotResults response.
	MaxGifCatalogEntries = MaxBotInlineResults
	// MaxGifCatalogUploadSize bounds one admin-uploaded catalog file.
	// 20MB matches MaxBotInlineWebSize, the size a client-side inline GIF
	// result is already allowed to be.
	MaxGifCatalogUploadSize = MaxBotInlineWebSize
)

// GifCatalogEntry is one admin-curated GIF served by the built-in @gif inline
// bot (see rpc.ServiceBotInlineResults) for the client's GIF picker
// trending/search panel. DocumentID references an already-uploaded document
// (see files.Service.AdminUploadGifMaterial) -- the catalog only tracks which
// documents are featured and in what order, it does not own the media itself.
type GifCatalogEntry struct {
	ID         int64
	Title      string
	DocumentID int64
	Enabled    bool
	SortOrder  int
	CreatedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
