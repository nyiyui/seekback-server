package server

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

type key struct{}

// LoginUserDataKey is the key for the login user data in the request context.
// When using mainLogin, this key will be set to the githubUserData struct.
var LoginUserDataKey key

// TimeLocationKey is the key for the *time.Location in the request context.
// When using mainLogin, this key will be set to the *time.Location set by the user.
var TimeLocationKey = "timeLocation"

func getTimeLocation(r *http.Request) *time.Location {
	loc, ok := r.Context().Value(TimeLocationKey).(*time.Location)
	if !ok {
		return time.UTC
	}
	return loc
}

func init() {
	gob.RegisterName("githubUserData", githubUserData{})
}

func (s *Server) mainLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loginSession, err := s.store.Get(r, "login")
		if err != nil {
			log.Printf("login session get: %s", err)
			http.Error(w, "session failure", 400)
			return
		}
		_, ok := loginSession.Values["githubUserData"]
		if !ok {
			http.Redirect(w, r, "/login", 302)
			return
		}
		data := loginSession.Values["githubUserData"].(githubUserData)
		if data.Login != s.mainUser {
			http.Error(w, "must be main user", 401)
			return
		}
		if _, ok := loginSession.Values["timezone"]; ok {
			tzName := loginSession.Values["timezone"].(string)
			loc, err := time.LoadLocation(tzName)
			if err != nil {
				http.Error(w, fmt.Sprintf("invalid timezone: %s", tzName), 500)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), TimeLocationKey, loc))
		}
		r = r.WithContext(context.WithValue(r.Context(), LoginUserDataKey, data))
		next.ServeHTTP(w, r)
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("code") {
		http.Error(w, "login page should not have query parameter `code' - make sure your redirect URI is set correctly.", 500)
		return
	}
	session, err := s.store.Get(r, "login-oauth2")
	if err != nil {
		log.Printf("session get: %s", err)
		http.Error(w, "session failure", 400)
		return
	}
	verifier := oauth2.GenerateVerifier()
	session.Values["verifier"] = verifier
	err = session.Save(r, w)
	if err != nil {
		log.Printf("session save: %s", err)
		http.Error(w, "session failure", 400)
		return
	}
	url := s.oauthConfig.AuthCodeURL("", oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, url, 302)
}

type githubUserData struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

func (s *Server) loginCallback(w http.ResponseWriter, r *http.Request) {
	session, err := s.store.Get(r, "login-oauth2")
	if err != nil {
		log.Printf("session get: %s", err)
		http.Error(w, "session failure", 400)
		return
	}
	loginSession, err := s.store.Get(r, "login")
	if err != nil {
		log.Printf("login session get: %s", err)
		http.Error(w, "session failure", 400)
		return
	}

	log.Printf("session: %s", session.Values)

	_, ok := session.Values["verifier"]
	if !ok {
		http.Error(w, "try logging in again", 400)
		return
	}

	verifier := session.Values["verifier"].(string)
	delete(session.Values, "verifier")
	code := r.URL.Query().Get("code")
	token, err := s.oauthConfig.Exchange(r.Context(), code, oauth2.VerifierOption(verifier))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	client := s.oauthConfig.Client(r.Context(), token)
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		http.Error(w, "failed to get user data from GitHub (request)", 500)
		return
	}
	if resp.StatusCode != 200 {
		http.Error(w, "failed to get user data from GitHub (response status code)", 500)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "failed to get user data from GitHub (response body)", 500)
		return
	}
	var data githubUserData
	err = json.Unmarshal(body, &data)
	if err != nil {
		http.Error(w, "failed to get user data from GitHub (response json)", 500)
		return
	}
	loginSession.Values["githubUserData"] = data
	err = session.Save(r, w)
	if err != nil {
		log.Printf("session save: %s", err)
		http.Error(w, "session failure", 400)
		return
	}
	err = loginSession.Save(r, w)
	if err != nil {
		log.Printf("login session save: %s", err)
		http.Error(w, "session failure", 400)
		return
	}
	http.Error(w, fmt.Sprintf("logged in as %s", data.Login), 200)
}

func (s *Server) loginSettings(w http.ResponseWriter, r *http.Request) {
	loginSession, err := s.store.Get(r, "login")
	if err != nil {
		log.Printf("login session get: %s", err)
		http.Error(w, "session failure", 400)
		return
	}
	if r.Method == "POST" {
		err = r.ParseForm()
		if err != nil {
			http.Error(w, "failed to parse form", 422)
			return
		}
		tzName := r.Form.Get("timezone")
		_, err := time.LoadLocation(tzName)
		if err != nil {
			http.Error(w, "invalid timezone", 422)
			return
		}
		loginSession.Values["timezone"] = tzName
		err = loginSession.Save(r, w)
		if err != nil {
			log.Printf("login session save: %s", err)
			http.Error(w, "session failure", 400)
			return
		}
	}
	var tzName string
	_, ok := loginSession.Values["timezone"]
	if ok {
		tzName = loginSession.Values["timezone"].(string)
	} else {
		tzName = ""
	}
	s.renderTemplate("login-settings.html", w, r, map[string]interface{}{
		"timezone": tzName,
	})
}
