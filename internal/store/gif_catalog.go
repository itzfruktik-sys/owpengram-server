package store

import (
	"context"

	"telesrv/internal/domain"
)

// GifCatalogStore owns the admin-curated GIF catalog the built-in @gif inline
// bot serves as results for the client's GIF picker.
type GifCatalogStore interface {
	// CreateGifCatalogEntry inserts a new entry. entry.ID must already be set
	// by the caller (same convention as documents/photos elsewhere in this
	// codebase -- ids are app-generated, not database-serial).
	CreateGifCatalogEntry(ctx context.Context, entry domain.GifCatalogEntry) (domain.GifCatalogEntry, error)
	// HasGifCatalogSourceFilename reports whether a seed-imported entry for
	// this filename already exists, so files.Service.SeedGifs can skip
	// re-transcoding a file it already imported on a previous startup.
	// Always false for an empty filename (that's the panel-upload sentinel,
	// never a real seed match).
	HasGifCatalogSourceFilename(ctx context.Context, filename string) (bool, error)
	// ListGifCatalog returns every entry ordered by (sort_order, id).
	// onlyEnabled=true is what @gif serves; the admin panel lists everything.
	ListGifCatalog(ctx context.Context, onlyEnabled bool) ([]domain.GifCatalogEntry, error)
	// SetGifCatalogEnabled toggles whether an entry is served. changed=false
	// if the id doesn't exist.
	SetGifCatalogEnabled(ctx context.Context, id int64, enabled bool) (bool, error)
	// SetGifCatalogSortOrder rewrites an entry's display position.
	SetGifCatalogSortOrder(ctx context.Context, id int64, order int) (bool, error)
	// SetGifCatalogCategory sets (or clears, via "") an entry's category.
	// changed=false if the id doesn't exist.
	SetGifCatalogCategory(ctx context.Context, id int64, category string) (bool, error)
	// DeleteGifCatalogEntry removes an entry. The referenced document is left
	// alone -- catalog membership, not the document itself, is what's deleted.
	DeleteGifCatalogEntry(ctx context.Context, id int64) (bool, error)
}
