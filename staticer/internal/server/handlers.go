package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/baely/staticer/internal/models"
	"github.com/baely/staticer/internal/storage"
	"github.com/baely/staticer/internal/wordgen"
)

var generator = wordgen.NewGenerator()

// populateURLs fills in the computed URL field for sites from the DB
func (s *Server) populateURLs(sites []*models.Site) {
	for _, site := range sites {
		if site.CustomDomain != "" {
			site.URL = "https://" + site.CustomDomain
		} else {
			site.URL = fmt.Sprintf("https://%s.%s", site.Subdomain, s.config.Host)
		}
	}
}

func (s *Server) jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *Server) jsonOK(w http.ResponseWriter, data interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

// handleDeploy handles POST /api/deploy
func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Auth
	if r.Header.Get("X-Upload-Secret") != s.config.UploadSecret {
		s.jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(s.config.MaxUploadSize); err != nil {
		s.jsonError(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		s.jsonError(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Determine subdomain
	subdomain := r.FormValue("subdomain")
	if subdomain == "" {
		subdomain, err = generator.GenerateUnique(s.store.SubdomainExists)
		if err != nil {
			s.jsonError(w, "failed to generate subdomain", http.StatusInternalServerError)
			return
		}
	} else if s.store.SubdomainExists(subdomain) {
		s.jsonError(w, "subdomain already exists", http.StatusConflict)
		return
	}

	// Parse options
	opts := &storage.DeployOptions{}

	if domain := r.FormValue("domain"); domain != "" {
		opts.CustomDomain = domain
	}

	if expires := r.FormValue("expires"); expires != "" {
		if expires != "never" {
			dur, err := parseDuration(expires)
			if err != nil {
				s.jsonError(w, "invalid expiration format", http.StatusBadRequest)
				return
			}
			t := time.Now().Add(dur)
			opts.ExpiresAt = &t
		}
	} else {
		// Default 24h expiration
		t := time.Now().Add(24 * time.Hour)
		opts.ExpiresAt = &t
	}

	if r.FormValue("listed") == "true" {
		opts.Listed = true
	}

	// Create site
	filename := header.Filename
	isZip := strings.HasSuffix(strings.ToLower(filename), ".zip")

	if isZip {
		result, err := s.store.CreateSite(subdomain, file, s.config.MaxFilesPerSite, s.config.MaxExtractedSize, s.config.Host, opts)
		if err != nil {
			s.logger.Error("Failed to create site", "error", err)
			s.jsonError(w, fmt.Sprintf("deploy failed: %v", err), http.StatusInternalServerError)
			return
		}
		// Schedule deletion if expiration set and Temporal available
		if result.ExpiresAt != nil && s.temporal != nil {
			delay := time.Until(*result.ExpiresAt)
			if err := s.temporal.ScheduleSiteDeletion(context.Background(), subdomain, result.CreatedAt, delay); err != nil {
				s.logger.Warn("Failed to schedule deletion", "subdomain", subdomain, "error", err)
			}
		}
		s.jsonOK(w, result, http.StatusCreated)
	} else {
		// Single file
		data, err := io.ReadAll(file)
		if err != nil {
			s.jsonError(w, "failed to read file", http.StatusInternalServerError)
			return
		}
		result, err := s.store.CreateSingleFileSite(subdomain, bytes.NewReader(data), "index.html", int64(len(data)), s.config.Host, opts)
		if err != nil {
			s.logger.Error("Failed to create single-file site", "error", err)
			s.jsonError(w, fmt.Sprintf("deploy failed: %v", err), http.StatusInternalServerError)
			return
		}
		if result.ExpiresAt != nil && s.temporal != nil {
			delay := time.Until(*result.ExpiresAt)
			if err := s.temporal.ScheduleSiteDeletion(context.Background(), subdomain, result.CreatedAt, delay); err != nil {
				s.logger.Warn("Failed to schedule deletion", "subdomain", subdomain, "error", err)
			}
		}
		s.jsonOK(w, result, http.StatusCreated)
	}
}

// handleSites handles GET /api/sites
func (s *Server) handleSites(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("X-Upload-Secret") != s.config.UploadSecret {
		s.jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sites, err := s.store.ListSites()
	if err != nil {
		s.jsonError(w, "failed to list sites", http.StatusInternalServerError)
		return
	}

	s.populateURLs(sites)

	s.jsonOK(w, map[string]interface{}{
		"sites": sites,
		"total": len(sites),
	}, http.StatusOK)
}

// handleSiteAction handles DELETE and PATCH /api/sites/{subdomain}
func (s *Server) handleSiteAction(w http.ResponseWriter, r *http.Request) {
	subdomain := strings.TrimPrefix(r.URL.Path, "/api/sites/")
	if subdomain == "" {
		s.jsonError(w, "missing subdomain", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		s.handleSiteDelete(w, r, subdomain)
	case http.MethodPatch:
		s.handleSiteUpdate(w, r, subdomain)
	default:
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSiteDelete(w http.ResponseWriter, r *http.Request, subdomain string) {
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		s.jsonError(w, "missing API key", http.StatusUnauthorized)
		return
	}

	valid, err := s.store.VerifyAPIKey(subdomain, apiKey)
	if err != nil || !valid {
		s.jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Cancel scheduled deletion if Temporal available
	if s.temporal != nil {
		s.temporal.CancelSiteDeletion(context.Background(), subdomain)
	}

	if err := s.store.DeleteSite(subdomain); err != nil {
		s.jsonError(w, "failed to delete site", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSiteUpdate(w http.ResponseWriter, r *http.Request, subdomain string) {
	// Auth via upload secret, admin secret, or API key
	authed := false
	if r.Header.Get("X-Upload-Secret") == s.config.UploadSecret {
		authed = true
	} else if r.Header.Get("X-Admin-Secret") == s.config.AdminSecret {
		authed = true
	} else if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		valid, err := s.store.VerifyAPIKey(subdomain, apiKey)
		if err == nil && valid {
			authed = true
		}
	}
	if !authed {
		s.jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		Listed      *bool   `json:"listed"`
		Title       *string `json:"title"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	updates := make(map[string]interface{})
	if body.Listed != nil {
		updates["listed"] = *body.Listed
	}
	if body.Title != nil {
		updates["title"] = *body.Title
	}
	if body.Description != nil {
		updates["description"] = *body.Description
	}

	if len(updates) > 0 {
		if err := s.store.UpdateSiteMetadata(subdomain, updates); err != nil {
			s.jsonError(w, "failed to update site", http.StatusInternalServerError)
			return
		}
	}

	site, err := s.store.GetSite(subdomain)
	if err != nil {
		s.jsonError(w, "failed to get site", http.StatusInternalServerError)
		return
	}
	s.populateURLs([]*models.Site{site})
	s.jsonOK(w, site, http.StatusOK)
}

// handlePublicSites handles GET /api/public/sites - no auth required
func (s *Server) handlePublicSites(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET")

	sites, err := s.store.ListPublicSites()
	if err != nil {
		s.jsonError(w, "failed to list sites", http.StatusInternalServerError)
		return
	}

	s.populateURLs(sites)

	s.jsonOK(w, map[string]interface{}{
		"sites": sites,
		"total": len(sites),
	}, http.StatusOK)
}

// handleAdminSites handles GET /api/admin/sites
func (s *Server) handleAdminSites(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("X-Admin-Secret") != s.config.AdminSecret {
		s.jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sites, err := s.store.ListSites()
	if err != nil {
		s.jsonError(w, "failed to list sites", http.StatusInternalServerError)
		return
	}

	s.populateURLs(sites)

	s.jsonOK(w, map[string]interface{}{
		"sites": sites,
		"total": len(sites),
	}, http.StatusOK)
}

// handleAdminSiteAction handles DELETE /api/admin/sites/{subdomain}
func (s *Server) handleAdminSiteAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("X-Admin-Secret") != s.config.AdminSecret {
		s.jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	subdomain := strings.TrimPrefix(r.URL.Path, "/api/admin/sites/")
	if subdomain == "" {
		s.jsonError(w, "missing subdomain", http.StatusBadRequest)
		return
	}

	if s.temporal != nil {
		s.temporal.CancelSiteDeletion(context.Background(), subdomain)
	}

	if err := s.store.DeleteSite(subdomain); err != nil {
		s.jsonError(w, "failed to delete site", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleAdminStats handles GET /api/admin/stats
func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("X-Admin-Secret") != s.config.AdminSecret {
		s.jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	stats, err := s.store.GetStorageStats()
	if err != nil {
		s.jsonError(w, "failed to get stats", http.StatusInternalServerError)
		return
	}

	s.jsonOK(w, stats, http.StatusOK)
}

// parseDuration parses durations like "1h", "7d", "30d"
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		var n int
		if _, err := fmt.Sscanf(days, "%d", &n); err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
