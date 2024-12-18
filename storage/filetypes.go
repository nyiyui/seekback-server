package storage

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
