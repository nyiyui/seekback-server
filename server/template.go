package server

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"math"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/google/safehtml"
	"github.com/google/safehtml/template"
	"github.com/google/safehtml/uncheckedconversions"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"nyiyui.ca/seekback-server/storage"
)

var buildInfo debug.BuildInfo
var vcsInfo string

//go:embed layouts
var layoutsFS embed.FS

//go:embed templates
var templatesFS embed.FS

type stringConstant string

func init() {
	buildInfo2, _ := debug.ReadBuildInfo()
	if buildInfo2 != nil {
		buildInfo = *buildInfo2
	}
}

func (s *Server) renderTemplate(path stringConstant, w http.ResponseWriter, r *http.Request, data map[string]interface{}) {
	t, ok := s.tps[string(path)]
	if !ok {
		panic("template not found")
		return
	}
	if data == nil {
		data = map[string]interface{}{}
	}
	data["login"], _ = r.Context().Value(LoginUserDataKey).(githubUserData)
	data["tzloc"] = getTimeLocation(r)
	err := t.Execute(w, data)
	if err != nil {
		log.Printf("template error: %s", err)
		http.Error(w, "template error", 500)
		return
	}
}

func (s *Server) parseTemplates() error {
	matches, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		return err
	}
	s.tps = map[string]*template.Template{}
	for _, match := range matches {
		_, basename := filepath.Split(match)
		s.tps[basename], err = s.parseTemplate(basename)
		if err != nil {
			return fmt.Errorf("parse %s: %w", basename, err)
		}
	}
	return nil
}

func (s *Server) parseTemplate(basename string) (*template.Template, error) {
	t := template.New(basename).
		Funcs(template.FuncMap(sprig.FuncMap())).
		Funcs(template.FuncMap{
			"styleTopHeight": func(top, height string) safehtml.Style {
				return safehtml.StyleFromProperties(safehtml.StyleProperties{
					Top:    top,
					Height: height,
				})
			},
			"genRange": func(count int) []int {
				items := make([]int, count)
				for i := 0; i < count; i++ {
					items[i] = i
				}
				return items
			},
			"renderMarkdown": func(s string) (safehtml.HTML, error) {
				md := goldmark.New(goldmark.WithExtensions(extension.Linkify))
				var buf bytes.Buffer
				err := md.Convert([]byte(s), &buf)
				if err != nil {
					return safehtml.HTML{}, err
				}
				return uncheckedconversions.HTMLFromStringKnownToSatisfyTypeContract(buf.String()), nil
			},
			"formatDayLong": func(loc *time.Location, t time.Time) string {
				return t.In(loc).Format("2006-01-02 Mon")
			},
			"formatDay": func(loc *time.Location, t time.Time) string {
				return t.In(loc).Format("2006-01-02")
			},
			"formatHM": func(loc *time.Location, t time.Time) string {
				return t.In(loc).Format("15:04")
			},
			"formatDatetimeLocalHTML": func(loc *time.Location, t time.Time) string {
				return t.In(loc).Format("2006-01-02T15:04")
			},
			"formatUser": func(loc *time.Location, t time.Time) string {
				t = t.In(loc)
				abs := t.Format("2006-01-02 15:04")
				rel := t.Sub(time.Now()).Round(time.Minute)
				rel2 := rel.String()
				return fmt.Sprintf("%s (%s)", abs, rel2[:len(rel2)-2])
			},
			"formatUserOrig": func(t time.Time) string {
				abs := t.Format("2006-01-02 15:04")
				rel := t.Sub(time.Now()).Round(time.Minute)
				rel2 := rel.String()
				return fmt.Sprintf("%s (%s)", abs, rel2[:len(rel2)-2])
			},
			"printTZ": func(t time.Time) string {
				name, offsetRaw := t.Zone()
				offset := time.Duration(offsetRaw) * time.Second
				offsetString := fmt.Sprintf("%02d:%02d", int(math.Abs(offset.Hours())), int(math.Abs(offset.Minutes()))%60)
				if offset.Hours() < 0 {
					offsetString = "-" + offsetString
				}
				if name == "" {
					return offsetString
				}
				return fmt.Sprintf("%s / %s", name, offset)
			},
			"filenameToMime": func(filename string) string {
				ext := filepath.Ext(filename)
				if len(ext) == 0 {
					return ""
				}
				return storage.MediaFileTypes[ext[1:]]
			},
			"buildInfo": func() debug.BuildInfo {
				return buildInfo
			},
			"vcsInfo": func() string {
				return vcsInfo
			},
		})
	t, err := t.ParseFS(template.TrustedFSFromEmbed(layoutsFS), "layouts/*.html")
	if err != nil {
		return nil, err
	}
	t, err = t.ParseFS(template.TrustedFSFromEmbed(templatesFS), fmt.Sprintf("templates/%s", basename))
	if err != nil {
		return nil, err
	}
	return t, nil
}
