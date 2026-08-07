package postgres

import (
	"context"
	"fmt"

	"telesrv/internal/domain"
	"telesrv/internal/store/postgres/sqlcgen"
)

type GifCatalogStore struct {
	db sqlcgen.DBTX
}

func NewGifCatalogStore(db sqlcgen.DBTX) *GifCatalogStore {
	return &GifCatalogStore{db: db}
}

func (s *GifCatalogStore) CreateGifCatalogEntry(ctx context.Context, entry domain.GifCatalogEntry) (domain.GifCatalogEntry, error) {
	if entry.ID == 0 || entry.DocumentID == 0 {
		return domain.GifCatalogEntry{}, fmt.Errorf("create gif catalog entry: id and document_id are required")
	}
	row := s.db.QueryRow(ctx, `
INSERT INTO gif_catalog (id, title, document_id, enabled, sort_order, created_by)
VALUES ($1, $2, $3, true, $4, $5)
RETURNING id, title, document_id, enabled, sort_order, created_by, created_at, updated_at`,
		entry.ID, entry.Title, entry.DocumentID, entry.SortOrder, entry.CreatedBy)
	out, err := scanGifCatalogEntry(row.Scan)
	if err != nil {
		return domain.GifCatalogEntry{}, fmt.Errorf("create gif catalog entry: %w", err)
	}
	return out, nil
}

func (s *GifCatalogStore) ListGifCatalog(ctx context.Context, onlyEnabled bool) ([]domain.GifCatalogEntry, error) {
	rows, err := s.db.Query(ctx, `
SELECT id, title, document_id, enabled, sort_order, created_by, created_at, updated_at
FROM gif_catalog
WHERE NOT $1 OR enabled
ORDER BY sort_order, id
LIMIT `+fmt.Sprint(domain.MaxGifCatalogEntries), onlyEnabled)
	if err != nil {
		return nil, fmt.Errorf("list gif catalog: %w", err)
	}
	defer rows.Close()
	out := make([]domain.GifCatalogEntry, 0)
	for rows.Next() {
		item, err := scanGifCatalogEntry(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan gif catalog entry: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *GifCatalogStore) SetGifCatalogEnabled(ctx context.Context, id int64, enabled bool) (bool, error) {
	tag, err := s.db.Exec(ctx, `UPDATE gif_catalog SET enabled = $2, updated_at = now() WHERE id = $1`, id, enabled)
	if err != nil {
		return false, fmt.Errorf("set gif catalog entry enabled: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s *GifCatalogStore) SetGifCatalogSortOrder(ctx context.Context, id int64, order int) (bool, error) {
	tag, err := s.db.Exec(ctx, `UPDATE gif_catalog SET sort_order = $2, updated_at = now() WHERE id = $1`, id, order)
	if err != nil {
		return false, fmt.Errorf("set gif catalog entry sort order: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s *GifCatalogStore) DeleteGifCatalogEntry(ctx context.Context, id int64) (bool, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM gif_catalog WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("delete gif catalog entry: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func scanGifCatalogEntry(scan func(dest ...any) error) (domain.GifCatalogEntry, error) {
	var e domain.GifCatalogEntry
	if err := scan(&e.ID, &e.Title, &e.DocumentID, &e.Enabled, &e.SortOrder, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return domain.GifCatalogEntry{}, err
	}
	return e, nil
}
