package storage

import (
	"context"
	"log"
	"os"
	"time"
)

// WatchAndSyncFiles watches the samples directory every interval seconds and calls SyncFiles if the directory has files newer than the last sync.
// Currently, there is no way to stop this method.
func (s *Storage) WatchAndSyncFiles(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastSync := time.Now()
	for range ticker.C {
		log.Println("WatchAndSyncFiles: checking samples directory...")
		entries, err := os.ReadDir(s.SamplesPath)
		if err != nil {
			log.Printf("WatchAndSyncFiles: read samples directory: %s", err)
			continue
		}
		var latestModified time.Time
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				log.Printf("WatchAndSyncFiles: get file info (ignoring this file): %s", err)
				continue
			}
			if info.ModTime().After(latestModified) {
				latestModified = info.ModTime()
			}
		}
		if latestModified.After(lastSync) {
			log.Printf("WatchAndSyncFiles: latest modified time (%s) is after last sync (%s), syncing files…", latestModified, lastSync)
			err := s.SyncFiles(context.Background())
			if err != nil {
				log.Printf("WatchAndSyncFiles: sync files: %s", err)
				continue
			} else {
				log.Printf("WatchAndSyncFiles: files synced.")
				lastSync = time.Now()
			}
		} else {
			log.Println("WatchAndSyncFiles: no new files.")
		}
	}
}
