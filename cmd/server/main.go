package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"nyiyui.ca/seekback-server/database"
	"nyiyui.ca/seekback-server/server"
	"nyiyui.ca/seekback-server/storage"
	"nyiyui.ca/seekback-server/tokens"
)

func getenvNonEmpty(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("%s is not set", key)
	}
	return value
}

func main() {
	var dbPath string
	var bindAddress string
	var tokensPath string
	flag.StringVar(&bindAddress, "bind", "127.0.0.1:8080", "bind address")
	flag.StringVar(&dbPath, "db-path", "db.sqlite3", "path to database")
	flag.StringVar(&tokensPath, "tokens-path", "", "path to tokens")
	flag.Parse()

	data, err := os.ReadFile(tokensPath)
	if err != nil {
		log.Fatal(err)
	}
	tokenMap := map[string]server.TokenInfo{}
	err = json.Unmarshal(data, &tokenMap)
	if err != nil {
		log.Fatal(err)
	}
	tokenMap2 := map[tokens.TokenHash]server.TokenInfo{}
	for k, v := range tokenMap {
		tokenMap2[tokens.MustParseTokenHash(k)] = v
	}

	log.Printf("opening database...")
	db, err := database.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("migrating database...")
	err = database.Migrate(db.DB)
	if err != nil && err != migrate.ErrNoChange {
		log.Fatal(err)
	}
	log.Printf("database migrated.")

	st := storage.New(getenvNonEmpty("SEEKBACK_SERVER_SAMPLES_PATH"), db)
	log.Printf("syncing files and database...")
	err = st.SyncFiles(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("files and database synced.")

	authKey, err := hex.DecodeString(getenvNonEmpty("SEEKBACK_SERVER_STORE_AUTH_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	store := sessions.NewFilesystemStore("", authKey)

	s, err := server.New(&oauth2.Config{
		ClientID:     getenvNonEmpty("SEEKBACK_SERVER_OAUTH_CLIENT_ID"),
		ClientSecret: getenvNonEmpty("SEEKBACK_SERVER_OAUTH_CLIENT_SECRET"),
		Scopes:       []string{},
		Endpoint:     github.Endpoint,
		RedirectURL:  getenvNonEmpty("SEEKBACK_SERVER_OAUTH_REDIRECT_URI"),
	}, store, "nyiyui", st, tokenMap2)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on %s...", bindAddress)
	log.Fatal(http.ListenAndServe(bindAddress, s))
}
