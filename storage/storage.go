package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
)

const SummaryExt = "txt"
const TranscriptExt = "vtt"

type Storage struct {
	SamplesPath string
	DB          *sqlx.DB
}

func New(samplesPath string, db *sqlx.DB) *Storage {
	return &Storage{
		SamplesPath: samplesPath,
		DB:          db,
	}
}

type HasSamplePreview interface {
	SamplePreview_() SamplePreview
}

type SamplePreview struct {
	ID         string
	Start      time.Time
	Duration   time.Duration
	Summary    string
	Transcript string
	Media      []string
}

func (sp SamplePreview) SamplePreview_() SamplePreview {
	return sp
}

type SamplePreviewWithSnippet struct {
	SamplePreview
	Snippet string `db:"snippet"`
}

func (spws SamplePreviewWithSnippet) SamplePreview_() SamplePreview {
	return spws.SamplePreview
}

func newSamplePreviewFromFilename(filename string) SamplePreview {
	name := filename[:len(filename)-len(filepath.Ext(filename))]
	sp := SamplePreview{ID: filename}
	t, err := time.Parse("2006-01-02T15:04:05-07:00", name)
	if err == nil {
		sp.Start = t
	}
	return sp
}

func (s *Storage) newSamplePreviewFromID(id string) (SamplePreview, error) {
	sp := SamplePreview{ID: id}
	t, err := time.Parse("2006-01-02T15:04:05-07:00", id)
	if err == nil {
		sp.Start = t
	}

	body, err := os.ReadFile(filepath.Join(s.SamplesPath, fmt.Sprintf("%s.%s", id, SummaryExt)))
	if err != nil && !os.IsNotExist(err) {
		return sp, err
	} else if err == nil {
		sp.Summary = string(body)
	}

	body, err = os.ReadFile(filepath.Join(s.SamplesPath, fmt.Sprintf("%s.%s", id, TranscriptExt)))
	if err != nil && !os.IsNotExist(err) {
		return sp, err
	} else if err == nil {
		sp.Transcript = string(body)
	}

	entries, err := os.ReadDir(s.SamplesPath)
	if err != nil {
		return sp, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		name := entry.Name()[:len(entry.Name())-len(ext)]
		if len(ext) == 0 {
			continue
		}
		if _, ok := MediaFileTypes[ext[1:]]; ok && name == id {
			sp.Media = append(sp.Media, entry.Name())
		}
	}

	return sp, nil
}

func (s *Storage) SamplePreviewList(ctx context.Context) ([]SamplePreview, error) {
	entries, err := os.ReadDir(s.SamplesPath)
	if err != nil {
		return nil, err
	}
	sps := map[string]SamplePreview{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		id := entry.Name()[:len(entry.Name())-len(ext)]
		if _, ok := sps[id]; ok {
			continue
		}
		if len(ext) == 0 {
			continue
		}
		if _, ok := MediaFileTypes[ext[1:]]; !ok {
			continue
		}
		sp, err := s.newSamplePreviewFromID(id)
		if err != nil {
			return nil, err
		}
		sps[id] = sp
	}
	sps2 := make([]SamplePreview, 0, len(sps))
	for _, sp := range sps {
		sps2 = append(sps2, sp)
	}
	return sps2, nil
}

func (s *Storage) SampleGet(id string) (SamplePreview, error) {
	return s.newSamplePreviewFromID(id)
}

func (s *Storage) SampleTranscriptSet(id string, transcript string, ctx context.Context) error {
	return os.WriteFile(filepath.Join(s.SamplesPath, fmt.Sprintf("%s%s", id, TranscriptExt)), []byte(transcript), 0644)
}

func (s *Storage) SampleFiles(id string) ([]string, error) {
	entries, err := os.ReadDir(s.SamplesPath)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		name := entry.Name()[:len(entry.Name())-len(ext)]
		if name == id {
			files = append(files, entry.Name())
		}
	}
	return files, nil
}

func (s *Storage) SyncFiles(ctx context.Context) error {
	sps, err := s.SamplePreviewList(ctx)
	if err != nil {
		return err
	}
	log.Printf("syncing %d samples...", len(sps))
	insertCount := 0
	updateCount := 0
	for i, sp := range sps {
		if i%100 == 0 {
			log.Printf("syncing %d/%d", i, len(sps))
		}
		var dummy SamplePreview
		err := s.DB.Get(&dummy, "SELECT * FROM samples WHERE id=?", sp.ID)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("select: %w", err)
		}
		if err == sql.ErrNoRows {
			_, err = s.DB.Exec("INSERT INTO samples (id, start, duration, summary, transcript) VALUES (?, ?, ?, ?, ?)", sp.ID, sp.Start, sp.Duration, sp.Summary, sp.Transcript)
			if err != nil {
				return fmt.Errorf("insert: %w", err)
			}
			insertCount++
		} else {
			_, err = s.DB.Exec("UPDATE samples SET start=?, duration=?, summary=?, transcript=? WHERE id=?", sp.Start, sp.Duration, sp.Summary, sp.Transcript, sp.ID)
			if err != nil {
				return fmt.Errorf("update: %w", err)
			}
			updateCount++
		}
	}
	log.Printf("synced %d samples, %d inserted, %d updated.", len(sps), insertCount, updateCount)
	_, err = s.DB.Exec("INSERT INTO samples_fts(samples_fts) VALUES ('rebuild')")
	if err != nil {
		return fmt.Errorf("fts rebuild: %w", err)
	}
	log.Printf("rebuilt fts index.")
	return nil
}

func (s *Storage) Search(query string, ctx context.Context) (sps []SamplePreviewWithSnippet, err error) {
	sps = make([]SamplePreviewWithSnippet, 0)
	err = s.DB.Select(&sps, `
SELECT * FROM samples JOIN (
  SELECT id, snippet(samples_fts, -1, '**', '**', '…', 64) AS snippet FROM samples_fts WHERE samples_fts MATCH ?
) USING (id)
`, query)
	if err != nil {
		return nil, err
	}
	return sps, nil
}
