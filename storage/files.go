package storage

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var MediaFileTypes = map[string]string{
	"aiff": "audio/aiff",
	"mp3":  "audio/mpeg",
}

var AllowedFileTypes []string

func init() {
	for k := range MediaFileTypes {
		AllowedFileTypes = append(AllowedFileTypes, k)
	}
	AllowedFileTypes = append(AllowedFileTypes, "vtt")
}

// getMediaDuration shells out to ffprobe to get the duration of the file at path.
// "file:" is prepended to the path, and passed to ffprobe like -i file:path.
func getMediaDuration(path string) (time.Duration, error) {
	// command from <https://stackoverflow.com/a/22243834>
	output, err := exec.Command("ffprobe",
		"-i", fmt.Sprintf("file:%s", path),
		"-show_entries", "format=duration",
		"-v", "quiet",
		"-of", "csv=p=0",
	).Output()
	if err != nil {
		return 0, fmt.Errorf("cmd: %w", err)
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(seconds) * time.Second, nil
}
