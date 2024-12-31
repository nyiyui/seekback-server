package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"time"

	"github.com/deiu/rdf2go"
	"github.com/gorilla/schema"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"nyiyui.ca/seekback-server/storage"
	"nyiyui.ca/seekback-server/tokens"

	"github.com/google/safehtml/template"
)

//go:embed static
var staticFS embed.FS

func composeFunc(handler http.HandlerFunc, middleware ...func(http.Handler) http.Handler) http.Handler {
	var h http.Handler = handler
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

type Server struct {
	mux         *http.ServeMux
	tps         map[string]*template.Template
	oauthConfig *oauth2.Config
	store       sessions.Store
	mainUser    string
	st          *storage.Storage
	tokens      map[tokens.TokenHash]TokenInfo
}

func newDecoder(r *http.Request) *schema.Decoder {
	decoder := schema.NewDecoder()
	decoder.RegisterConverter(time.Time{}, func(s string) reflect.Value {
		loc := getTimeLocation(r)
		t, err := time.ParseInLocation("2006-01-02T15:04", s, loc)
		if err != nil {
			return reflect.ValueOf(time.Time{})
		}
		return reflect.ValueOf(t)
	})
	decoder.RegisterConverter(time.Duration(0), func(s string) reflect.Value {
		d, err := time.ParseDuration(s)
		if err != nil {
			return reflect.ValueOf(time.Duration(0))
		}
		return reflect.ValueOf(d)
	})
	return decoder
}

func New(oauthConfig *oauth2.Config, store sessions.Store, adminUser string, st *storage.Storage, tokens map[tokens.TokenHash]TokenInfo) (*Server, error) {
	s := &Server{
		mux:         http.NewServeMux(),
		oauthConfig: oauthConfig,
		store:       store,
		mainUser:    adminUser,
		st:          st,
		tokens:      tokens,
	}
	err := s.setup()
	return s, err
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) setup() error {
	s.mux.HandleFunc("GET /login", s.login)
	s.mux.HandleFunc("GET /login/callback", s.loginCallback)
	s.mux.Handle("GET /login/settings", composeFunc(s.loginSettings, s.mainLogin))
	s.mux.Handle("POST /login/settings", composeFunc(s.loginSettings, s.mainLogin))

	s.mux.Handle("GET /rdf/all", composeFunc(s.getRDF, s.mainLogin))
	s.mux.Handle("GET /events", composeFunc(s.getEvents, s.apiAuthz(PermissionReadEvents)))

	s.mux.Handle("GET /samples", composeFunc(s.samplesView, s.mainLogin))
	s.mux.Handle("GET /sample/{id}", composeFunc(s.sampleView, s.mainLogin))
	s.mux.Handle("GET /file/{name}", composeFunc(s.fileServe, s.mainLogin))
	s.mux.Handle("POST /sample/{id}/transcript", composeFunc(s.sampleTranscriptPost, s.apiAuthz(PermissionWriteTranscript)))
	s.mux.Handle("POST /sample/{id}/summary", composeFunc(s.sampleSummaryPost, s.mainLogin))
	//s.mux.Handle("POST /sample/new", composeFunc(s.sampleNew, s.mainLogin))
	s.mux.Handle("GET /", http.FileServer(http.FS(staticFS)))
	err := s.parseTemplates()
	return err
}

func (s *Server) getRDF(w http.ResponseWriter, r *http.Request) {
	accept := r.Header.Get("Accept")
	if accept == "text/turtle" || accept == "application/ld+json" {
		w.Header().Set("Content-Type", accept)
	} else {
		accept = "text/turtle"
	}

	g := rdf2go.NewGraph("TODO")

	w.Header().Set("Content-Type", fmt.Sprintf("%s; charset=utf-8", accept))
	err := g.Serialize(w, accept)
	if err != nil {
		log.Printf("rdf serialization: %s", err)
		http.Error(w, "rdf serialization error", 500)
		return
	}
	return
}

type eventsQuery struct {
	TimeStart time.Time `schema:"time_start"`
	TimeEnd   time.Time `schema:"time_end"`
	Overlap   bool      `schema:"overlap"`
}

type Event struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

func (s *Server) getEvents(w http.ResponseWriter, r *http.Request) {
	decoder := schema.NewDecoder()
	decoder.RegisterConverter(time.Time{}, func(s string) reflect.Value {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return reflect.ValueOf(time.Time{})
		}
		return reflect.ValueOf(t)
	})
	var query eventsQuery
	err := decoder.Decode(&query, r.URL.Query())
	if err != nil {
		http.Error(w, fmt.Sprintf("query data decode failed: %s", err), 422)
		return
	}

	so := storage.SearchOptions{}
	if query.Overlap {
		so.SetOverlap(query.TimeStart, query.TimeEnd)
	} else {
		so.SetContained(query.TimeStart, query.TimeEnd)
	}

	var sps []storage.SamplePreviewWithSnippet
	sps, err = s.st.Search(so, r.Context())
	if err != nil {
		log.Printf("error getting sample list: %s", err)
		http.Error(w, "error getting sample list", 500)
		return
	}

	sort.Slice(sps, func(i, j int) bool {
		return sps[i].Start.After(sps[j].Start)
	})

	enc := json.NewEncoder(w)
	err = enc.Encode(sps)
	if err != nil {
		log.Printf("error encoding json: %s", err)
		http.Error(w, "error encoding json", 500)
		return
	}
}

type samplesViewQuery struct {
	TimeStart *time.Time `schema:"time_start"`
	TimeEnd   *time.Time `schema:"time_end"`
	Query     string     `schema:"query"`
}

func (s *Server) samplesView(w http.ResponseWriter, r *http.Request) {
	decoder := newDecoder(r)
	var query samplesViewQuery
	err := decoder.Decode(&query, r.URL.Query())
	if err != nil {
		http.Error(w, fmt.Sprintf("query data decode failed: %s", err), 422)
		return
	}

	if query.TimeStart != nil && *query.TimeStart == (time.Time{}) {
		query.TimeStart = nil
	}
	if query.TimeEnd != nil && *query.TimeEnd == (time.Time{}) {
		query.TimeEnd = nil
	}

	so := storage.SearchOptions{Query: query.Query}
	if query.TimeStart != nil && query.TimeEnd != nil {
		so.SetOverlap(*query.TimeStart, *query.TimeEnd)
	}

	sps, err := s.st.Search(so, r.Context())
	if err != nil {
		log.Printf("error getting sample list: %s", err)
		http.Error(w, fmt.Sprintf("error getting sample list: %s", err), 500)
		return
	}

	sort.Slice(sps, func(i, j int) bool {
		return sps[i].Start.After(sps[j].Start)
	})

	allSamplesHaveTranscripts := true
	for _, sp := range sps {
		if sp.Transcript == "" {
			allSamplesHaveTranscripts = false
			break
		}
	}
	s.renderTemplate("samples.html", w, r, map[string]interface{}{
		"query":                     query,
		"samples":                   sps,
		"allSamplesHaveTranscripts": allSamplesHaveTranscripts,
	})
}

func (s *Server) sampleView(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", 400)
		return
	}
	sample, err := s.st.SampleGet(id)
	if err != nil {
		log.Printf("error getting sample: %s", err)
		http.Error(w, "error getting sample", 500)
		return
	}
	so := storage.SearchOptions{}
	so.SetOverlap(sample.Start, *sample.End)
	overlaps, err := s.st.Search(so, r.Context())
	if err != nil {
		log.Printf("error getting overlaps: %s", err)
		http.Error(w, "error getting overlaps", 500)
		return
	}
	s.renderTemplate("sample.html", w, r, map[string]interface{}{
		"sample":   sample,
		"overlaps": overlaps,
	})
}

func (s *Server) sampleTranscriptPost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", 400)
		return
	}
	transcript, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "error reading transcript", 500)
		return
	}
	err = s.st.SampleTranscriptSet(id, string(transcript), r.Context())
	if err != nil {
		log.Printf("error setting transcript: %s", err)
		http.Error(w, "error setting transcript", 500)
		return
	}
	http.Error(w, "transcript set", 200)
}

type sampleSummaryPostQuery struct {
	Summary string `schema:"summary"`
}

func (s *Server) sampleSummaryPost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", 400)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, fmt.Sprintf("form parse failed: %s", err), 422)
		return
	}

	decoder := newDecoder(r)
	var query sampleSummaryPostQuery
	err = decoder.Decode(&query, r.PostForm)
	if err != nil {
		http.Error(w, fmt.Sprintf("query data decode failed: %s", err), 422)
		return
	}

	err = s.st.SampleSummarySet(id, query.Summary, r.Context())
	if err != nil {
		log.Printf("error setting transcript: %s", err)
		http.Error(w, "error setting transcript", 500)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/sample/%s", id), 302)
}

func (s *Server) fileServe(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing name", 400)
		return
	}
	ext := filepath.Ext(name)
	if !slices.Contains(storage.AllowedFileTypes, ext[1:]) {
		http.Error(w, "file type not allowed", 404)
		return
	}

	http.ServeFile(w, r, filepath.Join(s.st.SamplesPath, name))
}
