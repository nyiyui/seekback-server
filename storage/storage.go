package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
)

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

type SamplePreview struct {
	ID       string        `db:"id"`
	Start    time.Time     `db:"start"`
	Duration time.Duration `db:"duration"`
	Summary  string        `db:"summary"`
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

type Sample struct {
	SamplePreview
	Transcript string `db:"transcript"`
}

func (s *Storage) SamplePreviewList(ctx context.Context) ([]SamplePreview, error) {
	entries, err := os.ReadDir(s.SamplesPath)
	if err != nil {
		return nil, err
	}
	sps := make([]SamplePreview, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		sp := newSamplePreviewFromFilename(entry.Name())
		err := s.DB.GetContext(ctx, &sp, "SELECT id, start, duration, summary FROM samples WHERE id = ?", sp.ID)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("get: %w", err)
		}
		sps = append(sps, sp)
	}
	return sps, nil
}

func (s *Storage) SampleGet(id string, ctx context.Context) (*Sample, error) {
	sa := Sample{SamplePreview: newSamplePreviewFromFilename(id)}
	err := s.DB.GetContext(ctx, &sa, "SELECT id, start, duration, summary, transcript FROM samples WHERE id = ?", id)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get: %w", err)
	}
	return &sa, nil
}

func (s *Storage) SampleTranscriptSet(id string, transcript string, ctx context.Context) error {
	tx, err := s.DB.Beginx()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	var dummy Sample
	err = tx.GetContext(ctx, &dummy, "SELECT id FROM samples WHERE id = ?", id)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("get: %w", err)
	} else if err == sql.ErrNoRows {
		defaultStart := newSamplePreviewFromFilename(id).Start
		_, err := tx.ExecContext(ctx, "INSERT INTO samples (id, start, transcript) VALUES (?, ?, ?)", id, defaultStart, transcript)
		if err != nil {
			return fmt.Errorf("set default: %w", err)
		}
		return tx.Commit()
	} else {
		_, err := tx.ExecContext(ctx, "UPDATE samples SET transcript = ? WHERE id = ?", transcript, id)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		return tx.Commit()
	}
}
