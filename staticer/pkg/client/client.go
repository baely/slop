package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/baely/staticer/internal/models"
)

// Client is an API client for Staticer
type Client struct {
	ServerURL    string
	UploadSecret string
	HTTPClient   *http.Client
}

// New creates a new Staticer API client
func New(serverURL, uploadSecret string) *Client {
	return &Client{
		ServerURL:    serverURL,
		UploadSecret: uploadSecret,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Minute, // Allow time for large uploads
		},
	}
}

// DeployOptions holds optional parameters for deployment
type DeployOptions struct {
	Subdomain string // Custom subdomain (optional)
	Expires   string // Custom expiration like "1h", "7d", "never" (optional)
	Domain    string // Custom domain like "track.baileys.app" (optional)
	Listed    bool   // Whether the site should be publicly listed
}

// Deploy uploads a ZIP file and deploys a new site
func (c *Client) Deploy(zipData []byte, opts *DeployOptions) (*models.Site, error) {
	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add optional fields
	if opts != nil {
		if opts.Subdomain != "" {
			writer.WriteField("subdomain", opts.Subdomain)
		}
		if opts.Expires != "" {
			writer.WriteField("expires", opts.Expires)
		}
		if opts.Domain != "" {
			writer.WriteField("domain", opts.Domain)
		}
		if opts.Listed {
			writer.WriteField("listed", "true")
		}
	}

	// Add file part
	part, err := writer.CreateFormFile("file", "site.zip")
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := part.Write(zipData); err != nil {
		return nil, fmt.Errorf("failed to write file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create request
	url := c.ServerURL + "/api/deploy"
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Upload-Secret", c.UploadSecret)

	// Send request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		var errResp map[string]string
		if err := json.Unmarshal(respBody, &errResp); err == nil {
			if msg, ok := errResp["error"]; ok {
				return nil, fmt.Errorf("deploy failed: %s", msg)
			}
		}
		return nil, fmt.Errorf("deploy failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var site models.Site
	if err := json.Unmarshal(respBody, &site); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &site, nil
}

// Delete deletes a site
func (c *Client) Delete(subdomain, apiKey string) error {
	url := fmt.Sprintf("%s/api/sites/%s", c.ServerURL, subdomain)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-API-Key", apiKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// List retrieves all sites
func (c *Client) List() ([]*models.Site, error) {
	url := c.ServerURL + "/api/sites"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Upload-Secret", c.UploadSecret)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Sites []*models.Site `json:"sites"`
		Total int            `json:"total"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Sites, nil
}
