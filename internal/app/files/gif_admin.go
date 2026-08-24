package files

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"telesrv/internal/domain"
)

// ValidateGifUpload is a pure check (no store writes), used by a dry-run
// preview before AdminUploadGifMaterial actually materializes the document.
func (s *Service) ValidateGifUpload(fileName string, data []byte) (string, bool) {
	if len(data) == 0 || int64(len(data)) > domain.MaxGifCatalogUploadSize {
		return "", false
	}
	return detectGifCatalogUploadMime(data)
}

// AdminUploadGifMaterial turns a raw uploaded GIF/MP4 file into a loose
// Document not yet attached to any catalog entry -- the
// upload-bytes-into-a-document-row shape AdminUploadStickerMaterial uses for
// stickers, but routed through the same GIF->MP4 normalization the ordinary
// user upload path uses (normalizeUploadedGIF in photos.go).
//
// Transcoding is mandatory, not an optimization: a Telegram client only treats
// a document as a playable GIF when it is a silent H.264 MP4 carrying a video
// attribute (DocumentData::isGifv() requires mime video/mp4, and the inline
// GIF layout sizes each cell from document->dimensions). Storing the raw
// upload would produce a catalog entry the picker lays out at zero size and
// never animates, so both mime shapes go through the transcoder: it returns
// the canonical bytes plus the real width/height/duration the attributes need.
//
// MP4 input works even though the transcoder stages its input in a .gif temp
// file -- ffmpeg detects the format from content, not the extension -- and
// re-encoding it is what guarantees faststart/yuv420p/no-audio regardless of
// how the operator's file was produced.
func (s *Service) AdminUploadGifMaterial(ctx context.Context, fileName string, data []byte) (domain.Document, error) {
	if len(data) == 0 || int64(len(data)) > domain.MaxGifCatalogUploadSize {
		return domain.Document{}, domain.ErrGifCatalogFileInvalid
	}
	if _, ok := detectGifCatalogUploadMime(data); !ok {
		return domain.Document{}, domain.ErrGifCatalogFileInvalid
	}
	if s.gifs == nil {
		return domain.Document{}, fmt.Errorf("%w: ffmpeg/ffprobe are required to normalize a GIF for playback", domain.ErrGifCatalogFileInvalid)
	}
	converted, err := s.gifs.Transcode(ctx, data)
	if err != nil || len(converted.Data) == 0 || converted.Width <= 0 || converted.Height <= 0 {
		s.log.Warn("admin GIF catalog upload conversion failed", zap.Int("input_bytes", len(data)), zap.Error(err))
		return domain.Document{}, domain.ErrGifCatalogFileInvalid
	}
	objectKey, err := s.blobs.Put(ctx, converted.Data)
	if err != nil {
		return domain.Document{}, err
	}
	sum := sha256.Sum256(converted.Data)
	docID := randomID()
	if err := s.media.PutFileBlob(ctx, domain.FileBlob{
		LocationKey: fmt.Sprintf("doc:%d", docID),
		Backend:     domain.MediaBackend(s.blobs.Name()),
		ObjectKey:   objectKey,
		Size:        int64(len(converted.Data)),
		SHA256:      append([]byte(nil), sum[:]...),
		MimeType:    "video/mp4",
	}); err != nil {
		return domain.Document{}, err
	}
	name := strings.TrimSpace(fileName)
	if name == "" {
		name = "animation.gif"
	}
	doc := domain.Document{
		ID:            docID,
		AccessHash:    randomID(),
		FileReference: randomFileReference(),
		Date:          int(time.Now().Unix()),
		MimeType:      "video/mp4",
		Size:          int64(len(converted.Data)),
		DCID:          s.dc,
		Attributes: canonicalGIFVideoAttributes(
			[]domain.DocumentAttribute{{Kind: domain.DocAttrFilename, FileName: name}},
			converted, false),
	}
	if err := s.media.PutDocument(ctx, doc); err != nil {
		return domain.Document{}, err
	}
	return doc, nil
}

// detectGifCatalogUploadMime accepts exactly what an inline "gif" result is
// allowed to carry (see inlineExternalContentMimeAllowed): a real GIF, or an
// MP4 (Telegram normalizes animated GIFs to silent MP4 for delivery, so an
// operator uploading an already-converted MP4 is the common case, not an
// edge case).
func detectGifCatalogUploadMime(data []byte) (string, bool) {
	switch {
	case len(data) >= 6 && (string(data[0:6]) == "GIF87a" || string(data[0:6]) == "GIF89a"):
		return "image/gif", true
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		return "video/mp4", true
	default:
		return "", false
	}
}

// AdminCreateGifCatalogEntry adds an already-materialized document (from
// AdminUploadGifMaterial) to the catalog @gif serves.
func (s *Service) AdminCreateGifCatalogEntry(ctx context.Context, title string, documentID int64) (domain.GifCatalogEntry, error) {
	return s.createGifCatalogEntry(ctx, title, documentID, "")
}

// createGifCatalogEntry is the shared insert path for both the admin-panel
// upload (AdminCreateGifCatalogEntry, sourceFilename="") and the filesystem
// seed import (SeedGifs, sourceFilename=the imported file's name).
func (s *Service) createGifCatalogEntry(ctx context.Context, title string, documentID int64, sourceFilename string) (domain.GifCatalogEntry, error) {
	if s.gifCatalog == nil {
		return domain.GifCatalogEntry{}, domain.ErrGifCatalogUnavailable
	}
	title = strings.TrimSpace(title)
	if len(title) > domain.MaxGifCatalogTitleLen || documentID == 0 {
		return domain.GifCatalogEntry{}, domain.ErrGifCatalogEntryInvalid
	}
	if _, found, err := s.media.GetDocument(ctx, documentID); err != nil {
		return domain.GifCatalogEntry{}, err
	} else if !found {
		return domain.GifCatalogEntry{}, domain.ErrGifCatalogEntryInvalid
	}
	entry, err := s.gifCatalog.CreateGifCatalogEntry(ctx, domain.GifCatalogEntry{
		ID:             randomID(),
		Title:          title,
		DocumentID:     documentID,
		SourceFilename: sourceFilename,
	})
	if err != nil {
		return domain.GifCatalogEntry{}, err
	}
	return entry, nil
}

// AdminListGifCatalog returns every entry (enabled and disabled), unbounded,
// for the admin panel's list view.
func (s *Service) AdminListGifCatalog(ctx context.Context) ([]domain.GifCatalogEntry, error) {
	if s.gifCatalog == nil {
		return nil, domain.ErrGifCatalogUnavailable
	}
	return s.gifCatalog.ListGifCatalog(ctx, false, 0)
}

// ListGifCatalog is bots.gifCatalogSource's read: onlyEnabled=true is what
// @gif actually serves. Deliberately unbounded, not capped at
// domain.MaxGifCatalogEntries here -- bots.Service filters/ranks by category
// or query text over the *whole* catalog and only then takes the top
// MaxGifCatalogEntries for one response. Capping the fetch itself would
// silently limit that filtering to an arbitrary (sort_order, id) slice of a
// large catalog, e.g. a category tap finding none of its members among the
// first 50 rows and falling back to showing the unfiltered catalog instead.
func (s *Service) ListGifCatalog(ctx context.Context, onlyEnabled bool) ([]domain.GifCatalogEntry, error) {
	if s.gifCatalog == nil {
		return nil, nil
	}
	return s.gifCatalog.ListGifCatalog(ctx, onlyEnabled, 0)
}

// AdminSetGifCatalogEnabled toggles whether an entry is served.
func (s *Service) AdminSetGifCatalogEnabled(ctx context.Context, id int64, enabled bool) (bool, error) {
	if s.gifCatalog == nil {
		return false, domain.ErrGifCatalogUnavailable
	}
	changed, err := s.gifCatalog.SetGifCatalogEnabled(ctx, id, enabled)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, domain.ErrGifCatalogEntryNotFound
	}
	return true, nil
}

// AdminSetGifCatalogSortOrder rewrites an entry's display position.
func (s *Service) AdminSetGifCatalogSortOrder(ctx context.Context, id int64, order int) (bool, error) {
	if s.gifCatalog == nil {
		return false, domain.ErrGifCatalogUnavailable
	}
	changed, err := s.gifCatalog.SetGifCatalogSortOrder(ctx, id, order)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, domain.ErrGifCatalogEntryNotFound
	}
	return true, nil
}

// AdminSetGifCatalogCategory sets (or clears, via "") an entry's category.
func (s *Service) AdminSetGifCatalogCategory(ctx context.Context, id int64, category string) (bool, error) {
	if s.gifCatalog == nil {
		return false, domain.ErrGifCatalogUnavailable
	}
	if !domain.ValidGifCatalogCategory(category) {
		return false, domain.ErrGifCatalogEntryInvalid
	}
	changed, err := s.gifCatalog.SetGifCatalogCategory(ctx, id, category)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, domain.ErrGifCatalogEntryNotFound
	}
	return true, nil
}

// AdminAutoCategorizeGifCatalog runs ClassifyGifCategory against every
// currently-uncategorized entry's title and assigns whatever category it
// guesses (category stays "" -- i.e. the entry is left for manual tagging --
// when nothing matches). Already-categorized entries are left untouched, so
// this is safe to re-run after every new batch of GIFs lands (seeded or
// admin-uploaded) without clobbering an operator's manual corrections.
// Returns how many entries got a category assigned.
func (s *Service) AdminAutoCategorizeGifCatalog(ctx context.Context) (int, error) {
	if s.gifCatalog == nil {
		return 0, domain.ErrGifCatalogUnavailable
	}
	entries, err := s.gifCatalog.ListGifCatalog(ctx, false, 0)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, e := range entries {
		if e.Category != "" {
			continue
		}
		category := ClassifyGifCategory(e.Title)
		if category == "" {
			continue
		}
		ok, err := s.gifCatalog.SetGifCatalogCategory(ctx, e.ID, category)
		if err != nil {
			return changed, err
		}
		if ok {
			changed++
		}
	}
	return changed, nil
}

// AdminDeleteGifCatalogEntry removes an entry from the catalog. The
// referenced document is left alone.
func (s *Service) AdminDeleteGifCatalogEntry(ctx context.Context, id int64) (bool, error) {
	if s.gifCatalog == nil {
		return false, domain.ErrGifCatalogUnavailable
	}
	changed, err := s.gifCatalog.DeleteGifCatalogEntry(ctx, id)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, domain.ErrGifCatalogEntryNotFound
	}
	return true, nil
}
