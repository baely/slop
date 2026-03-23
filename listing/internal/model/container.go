package model

// Label constants for container metadata
const (
	LabelURL         = "baileys.public.url"
	LabelTitle       = "baileys.public.title"
	LabelDescription = "baileys.public.description"
)

// Container represents a Docker container with public labels
type Container struct {
	ID          string
	Name        string
	URL         string
	Title       string
	Description string
}

// Service is a unified type for the API response, representing either
// a Docker container or a static site
type Service struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"` // "container" or "static-site"
}
