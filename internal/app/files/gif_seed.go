package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"go.uber.org/zap"

	"telesrv/internal/domain"
)

// GifSeedStats reports one SeedGifs run's outcome.
type GifSeedStats struct {
	Imported int
	Skipped  int
	Failed   int
}

// SeedGifs imports every .gif/.mp4 file directly under root into the
// admin-curated GIF catalog @gif serves, so an operator can populate the
// catalog by just dropping files into a folder -- the same "drop it in
// data/<seed dir>, it shows up on next start" workflow SeedMedia already
// gives sticker/emoji packs, minus the export-manifest format that needs
// (regular sticker seeding carries fixed document ids/access hashes from an
// external export; there's no equivalent authority for a GIF an operator just
// found, so files here get a fresh app-generated id every import instead).
//
// A missing root is skipped, not an error -- same convention as SeedMedia and
// SeedPremiumPromo, since not every deployment wants a curated GIF catalog.
// Each file is imported at most once across restarts: entry.SourceFilename
// records the file's base name, and a file whose name already has a catalog
// row is skipped without re-reading or re-transcoding it. Renaming a file on
// disk therefore re-imports it as a new entry -- there's no content-hash
// dedup, only the filename-based one HasGifCatalogSourceFilename provides.
func (s *Service) SeedGifs(ctx context.Context, root string) (GifSeedStats, error) {
	var stats GifSeedStats
	if root == "" {
		return stats, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			if s.log != nil {
				s.log.Warn("gif seed directory does not exist, skipping gif catalog import (set TELESRV_GIF_SEED_DIR)",
					zap.String("dir", root))
			}
			return stats, nil
		}
		return stats, fmt.Errorf("read gif seed dir: %w", err)
	}
	if s.gifCatalog == nil {
		if s.log != nil {
			s.log.Warn("gif catalog store is not configured, skipping gif seed import", zap.String("dir", root))
		}
		return stats, nil
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".gif" || ext == ".mp4" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names) // deterministic import/log order across runs

	for _, name := range names {
		imported, err := s.seedOneGif(ctx, root, name)
		switch {
		case err != nil:
			stats.Failed++
			if s.log != nil {
				s.log.Warn("gif seed import failed", zap.String("file", name), zap.Error(err))
			}
		case imported:
			stats.Imported++
		default:
			stats.Skipped++
		}
	}
	return stats, nil
}

func (s *Service) seedOneGif(ctx context.Context, root, name string) (imported bool, err error) {
	has, err := s.gifCatalog.HasGifCatalogSourceFilename(ctx, name)
	if err != nil {
		return false, err
	}
	if has {
		return false, nil
	}
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return false, err
	}
	doc, err := s.AdminUploadGifMaterial(ctx, name, data)
	if err != nil {
		return false, err
	}
	if _, err := s.createGifCatalogEntry(ctx, gifTitleFromFilename(name), doc.ID, name); err != nil {
		return false, err
	}
	return true, nil
}

// gifTitleFromFilename derives a display title from a seed file's base name:
// strip the extension, replace separators with spaces, and title-case each
// word -- "cat_jumping-2.gif" becomes "Cat Jumping 2".
func gifTitleFromFilename(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.Map(func(r rune) rune {
		if r == '_' || r == '-' {
			return ' '
		}
		return r
	}, base)
	words := strings.Fields(base)
	for i, w := range words {
		// []rune, not w[:1]/w[1:]: byte-slicing would corrupt any multi-byte
		// first character (Cyrillic filenames are the expected case here, not
		// an edge case).
		r := []rune(w)
		words[i] = string(unicode.ToUpper(r[0])) + string(r[1:])
	}
	title := strings.Join(words, " ")
	// MaxGifCatalogTitleLen is a byte budget (createGifCatalogEntry checks
	// len(title) directly), so truncate by bytes too -- but back off to the
	// last full rune so a multi-byte character never gets split in half.
	for len(title) > domain.MaxGifCatalogTitleLen {
		title = string([]rune(title)[:len([]rune(title))-1])
	}
	return title
}
