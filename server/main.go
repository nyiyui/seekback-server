package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"time"

	"github.com/deiu/rdf2go"
	"github.com/gorilla/schema"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"nyiyui.ca/seekback-server/storage"
	"nyiyui.ca/seekback-server/tokens"

	"github.com/google/safehtml/template"
)

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
			return reflect.ValueOf(time.Now())
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

	s.mux.Handle("GET /samples", composeFunc(s.samplesView, s.mainLogin))
	s.mux.Handle("GET /sample/{id}", composeFunc(s.sampleView, s.mainLogin))
	s.mux.Handle("POST /sample/{id}/transcript", composeFunc(s.sampleTranscriptPost, s.apiAuthz(PermissionWriteTranscript)))
	//s.mux.Handle("POST /sample/new", composeFunc(s.sampleNew, s.mainLogin))
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

func (s *Server) samplesView(w http.ResponseWriter, r *http.Request) {
	sps, err := s.st.SamplePreviewList(r.Context())
	if err != nil {
		log.Printf("error getting sample list: %s", err)
		http.Error(w, "error getting sample list", 500)
		return
	}
	s.renderTemplate("samples.html", w, r, map[string]interface{}{
		"samples": sps,
	})
}

func (s *Server) sampleView(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", 400)
		return
	}
	sample, err := s.st.SampleGet(id, r.Context())
	if err != nil {
		log.Printf("error getting sample: %s", err)
		http.Error(w, "error getting sample", 500)
		return
	}
	s.renderTemplate("sample.html", w, r, map[string]interface{}{
		"sample": sample,
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
