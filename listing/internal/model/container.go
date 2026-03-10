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
