package server

import (
	"net/http"
	"slices"

	"nyiyui.ca/seekback-server/tokens"
)

type TokenHash string

type TokenInfo struct {
	Permissions []Permission
}

type Permission string

const (
	PermissionWriteTranscript Permission = "write:transcript"
)

func (s *Server) apiAuthz(permissionsRequired ...Permission) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := tokens.ParseToken(r.Header.Get("X-API-Token"))
			if err != nil {
				http.Error(w, "invalid token format", 400)
				return
			}
			tokenInfo := s.tokens[token.Hash()]
			for _, permissionRequired := range permissionsRequired {
				if !slices.Contains(tokenInfo.Permissions, permissionRequired) {
					http.Error(w, "insufficient permissions", 403)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
