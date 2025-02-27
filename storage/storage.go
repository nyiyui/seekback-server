package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
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
	End        *time.Time
	Summary    string
	Transcript string
	Media      []string
}

func (sp SamplePreview) SamplePreview_() SamplePreview {
	return sp
}

func (sp SamplePreview) TimeRange() (start, end time.Time) {
	if sp.End != nil {
		return sp.Start, *sp.End
	}
	return sp.Start, sp.Start.Add(sp.Duration)
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

// SamplePreviewList returns a list of SamplePreview structs from the samples directory.
// This method does not access the SQL database.
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
	sp, err := s.newSamplePreviewFromID(id)
	if err != nil {
		return SamplePreview{}, err
	}
	err = s.DB.Get(&sp, "SELECT * FROM samples WHERE id=?", id)
	if err != nil {
		return SamplePreview{}, err
	}
	return sp, nil
}

func (s *Storage) SampleTranscriptSet(id string, transcript string, ctx context.Context) error {
	return os.WriteFile(filepath.Join(s.SamplesPath, fmt.Sprintf("%s%s", id, TranscriptExt)), []byte(transcript), 0644)
}

func (s *Storage) SampleSummarySet(id string, transcript string, ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE samples SET summary=? WHERE id=?", transcript, id)
	return err
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

func (s *Storage) setEnds(ctx context.Context) error {
	log.Printf("setting end times...")
	sps := make([]SamplePreview, 0)
	err := s.DB.Select(&sps, "SELECT id, start, duration FROM samples WHERE end IS NULL")
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	for _, sp := range sps {
		_, err = s.DB.Exec("UPDATE samples SET end=? WHERE id=?", sp.Start.Add(sp.Duration), sp.ID)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
	}
	log.Printf("set end times for %d samples.", len(sps))
	return nil
}

// SyncFiles syncs the SQL database with files in the samples directory.
func (s *Storage) SyncFiles(ctx context.Context) error {
	log.Print("reading samples directory...")
	sps, err := s.SamplePreviewList(ctx)
	if err != nil {
		return err
	}
	log.Printf("syncing %d samples...", len(sps))
	insertCount := 0
	updateCount := 0
	durationCount := 0
	fsIDs := make([]string, 0, len(sps))
	start := time.Now()
	lastLog := start
	for i, sp := range sps {
		if time.Since(lastLog) > 5*time.Second {
			perSP := time.Since(start) / time.Duration(i+1)
			total := (perSP * time.Duration(len(sps)))
			eta := start.Add(total)
			log.Printf("syncing %d/%d ; %s to eta (%d inserted, %d updated, %d duration)", i, len(sps), time.Until(eta).Round(time.Second), insertCount, updateCount, durationCount)
			lastLog = time.Now()
		}

		fsIDs = append(fsIDs, sp.ID)
		var oldSP SamplePreview
		err := s.DB.Get(&oldSP, "SELECT * FROM samples WHERE id=?", sp.ID)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("select: %w", err)
		}
		if err == sql.ErrNoRows {
			durationCount++
			duration, err := getMediaDuration(filepath.Join(s.SamplesPath, sp.Media[0]))
			if err != nil {
				log.Printf("get media duration of %s failed: %s", sp.Media[0], err)
			} else {
				sp.Duration = duration
			}
			_, err = s.DB.Exec("INSERT INTO samples (id, start, duration, summary, transcript) VALUES (?, ?, ?, ?, ?)", sp.ID, sp.Start, sp.Duration, sp.Summary, sp.Transcript)
			if err != nil {
				return fmt.Errorf("insert: %w", err)
			}
			insertCount++
		} else {
			if oldSP.Duration == 0 && len(sp.Media) != 0 {
				durationCount++
				duration, err := getMediaDuration(filepath.Join(s.SamplesPath, sp.Media[0]))
				if err != nil {
					log.Printf("get media duration of %s failed: %s", sp.Media[0], err)
				} else {
					sp.Duration = duration
				}
			} else if oldSP.Duration != 0 {
				sp.Duration = oldSP.Duration
			}
			// Summary is not given from samples directory, so ignore.
			_, err = s.DB.Exec("UPDATE samples SET start=?, duration=?,  transcript=? WHERE id=?", sp.Start, sp.Duration, sp.Transcript, sp.ID)
			if err != nil {
				return fmt.Errorf("update: %w", err)
			}
			updateCount++
		}
	}
	dbIDs := make([]string, 0, len(sps))
	err = s.DB.Select(&dbIDs, "SELECT id FROM samples")
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	notInFS := setMinus(dbIDs, fsIDs)
	deleteCount := len(notInFS)
	for _, id := range notInFS {
		_, err = s.DB.Exec("DELETE FROM samples WHERE id=?", id)
		if err != nil {
			return fmt.Errorf("delete: %w", err)
		}
	}

	log.Printf("synced %d samples, %d inserted, %d updated, %d deleted (%d new media duration).", len(sps), insertCount, updateCount, deleteCount, durationCount)
	_, err = s.DB.Exec("INSERT INTO samples_fts(samples_fts) VALUES ('rebuild')")
	if err != nil {
		return fmt.Errorf("fts rebuild: %w", err)
	}
	log.Printf("rebuilt fts index.")
	err = s.setEnds(ctx)
	if err != nil {
		return fmt.Errorf("set ends: %w", err)
	}
	return nil
}

// Returns a set minus b.
func setMinus(a, b []string) []string {
	slices.Sort(a)
	slices.Sort(b)
	result := make([]string, 0)
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			i++
			j++
		} else if a[i] > b[j] {
			j++
		} else {
			result = append(result, a[i])
			i++
		}
	}
	for ; i < len(a); i++ {
		result = append(result, a[i])
	}
	return result
}

type SearchOptions struct {
	Query                   string
	StartAfter, StartBefore *time.Time
	EndAfter, EndBefore     *time.Time
}

func (so *SearchOptions) SetOverlap(start, end time.Time) {
	so.EndAfter = &start
	so.StartBefore = &end
}

func (so *SearchOptions) SetContained(start, end time.Time) {
	so.StartAfter = &start
	so.EndBefore = &end
}

func (s *Storage) Search(so SearchOptions, ctx context.Context) (sps []SamplePreviewWithSnippet, err error) {
	var query string
	var args []interface{}
	if so.Query == "" {
		query = `
SELECT * FROM samples
`
	} else {
		query = `
SELECT * FROM samples JOIN (
  SELECT id, snippet(samples_fts, -1, '**', '**', '…', 64) AS snippet FROM samples_fts WHERE samples_fts MATCH ? ORDER BY rank
) USING (id)
`
		args = append(args, so.Query)
	}
	query += "WHERE TRUE "
	if so.StartAfter != nil {
		query += "AND unixepoch(start) >= ?"
		args = append(args, so.StartAfter.Unix())
	}
	if so.StartBefore != nil {
		query += "AND unixepoch(start) <= ?"
		args = append(args, so.StartBefore.Unix())
	}
	if so.EndAfter != nil {
		query += "AND unixepoch(end) >= ?"
		args = append(args, so.EndAfter.Unix())
	}
	if so.EndBefore != nil {
		query += "AND unixepoch(end) <= ?"
		args = append(args, so.EndBefore.Unix())
	}

	sps = make([]SamplePreviewWithSnippet, 0)
	err = s.DB.Select(&sps, query, args...)
	if err != nil {
		return nil, err
	}
	return sps, nil
}
