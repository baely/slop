package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/baely/listing/internal/model"
	"github.com/baely/listing/internal/staticer"
)

type indexData struct {
	Containers  []model.Container
	StaticSites []staticer.Site
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	containers := s.store.List()

	data := indexData{
		Containers: containers,
	}
	if s.staticer != nil {
		data.StaticSites = s.staticer.List()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.template.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleAPISites(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var services []model.Service

	// Add containers
	for _, c := range s.store.List() {
		title := c.Title
		if title == "" {
			title = c.Name
		}
		services = append(services, model.Service{
			Title:       title,
			URL:         c.URL,
			Description: c.Description,
			Type:        "container",
		})
	}

	// Add static sites
	if s.staticer != nil {
		for _, site := range s.staticer.List() {
			title := site.Title
			if title == "" {
				title = site.Subdomain
			}
			services = append(services, model.Service{
				Title:       title,
				URL:         site.URL,
				Description: site.Description,
				Type:        "static-site",
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"services": services,
		"total":    len(services),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
