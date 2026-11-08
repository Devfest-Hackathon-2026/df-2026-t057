package app

import (
	"net/http"
	"strings"

	"google.golang.org/api/idtoken"
)

func (s *Server) allowedOrigin(origin string) bool {
	if origin == s.AppURL {
		return true
	}
	id, ok := strings.CutPrefix(origin, "chrome-extension://")
	if !ok || len(id) != 32 || strings.Trim(id, "abcdefghijklmnop") != "" {
		return false
	}
	if s.Mode == "demo" {
		return true
	}
	for _, allowed := range s.ExtensionIDs {
		if id == allowed {
			return true
		}
	}
	return false
}
func (s *Server) authorizeMaintenance(r *http.Request) bool {
	token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !bearer {
		return false
	}
	if s.SchedulerSecret != "" && token == s.SchedulerSecret {
		return true
	}
	if s.MaintenanceEmail == "" {
		return false
	}
	identity, err := idtoken.Validate(r.Context(), token, s.AppURL)
	if err != nil {
		return false
	}
	return identity.Claims["email"] == s.MaintenanceEmail && identity.Claims["email_verified"] == true
}
