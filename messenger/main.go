package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
)

type config struct {
	baseURL   string
	accountID string
	inboxID   string
	token     string
}

type cached struct {
	contactID      int
	sourceID       string
	conversationID int
}

var (
	cfg     config
	cache   = map[string]*cached{}
	cacheMu sync.Mutex
)

func main() {
	cfg = config{
		baseURL:   os.Getenv("CHATWOOT_URL"),
		accountID: os.Getenv("CHATWOOT_ACCOUNT_ID"),
		inboxID:   os.Getenv("CHATWOOT_INBOX_ID"),
		token:     os.Getenv("CHATWOOT_API_TOKEN"),
	}
	if cfg.baseURL == "" || cfg.accountID == "" || cfg.inboxID == "" || cfg.token == "" {
		log.Fatal("CHATWOOT_URL, CHATWOOT_ACCOUNT_ID, CHATWOOT_INBOX_ID, and CHATWOOT_API_TOKEN must be set")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", handler)
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

const indexPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Hermes</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: system-ui, -apple-system, sans-serif; max-width: 640px; margin: 0 auto; padding: 24px 16px; color: #111; font-size: 13px; line-height: 1.5; }
  .page-header { padding-bottom: 24px; margin-bottom: 24px; border-bottom: 1px solid #e0e0e0; }
  h1 { font-size: 18px; font-weight: 600; margin-bottom: 4px; }
  .subtitle { font-size: 12px; color: #999; margin: 0; }
  p { margin-bottom: 12px; color: #666; font-size: 12px; }
  pre { background: #fafafa; border: 1px solid #e0e0e0; padding: 14px; overflow-x: auto; font-family: 'Courier New', monospace; font-size: 12px; line-height: 1.6; margin-bottom: 20px; }
  .form-group { margin-bottom: 12px; }
  .form-group label { display: block; font-size: 11px; color: #999; margin-bottom: 4px; text-transform: uppercase; letter-spacing: 0.05em; }
  .form-group input, .form-group textarea { width: 100%; padding: 7px 9px; border: 1px solid #ddd; font-size: 13px; font-family: inherit; color: #111; outline: none; }
  .form-group input:focus, .form-group textarea:focus { border-color: #111; }
  .form-group input::placeholder, .form-group textarea::placeholder { color: #bbb; }
  textarea { resize: vertical; min-height: 80px; }
  button { display: block; width: 100%; padding: 8px; background: #111; color: #fff; border: 1px solid #111; font-size: 12px; font-family: inherit; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; cursor: pointer; margin-top: 6px; }
  button:hover:not(:disabled) { background: #333; border-color: #333; }
  button:disabled { opacity: 0.4; cursor: not-allowed; }
  #result { margin-top: 12px; padding: 6px 10px; font-size: 12px; display: none; }
  #result.ok { display: block; background: #fff; border: 1px solid #ddd; color: #111; }
  #result.err { display: block; background: #fff8f8; border: 1px solid #f5c0c0; color: #c00; }
</style>
</head>
<body>
<div class="page-header">
  <h1>Hermes</h1>
  <p class="subtitle">Message API</p>
</div>

<pre><code>curl -X POST https://hermes.baileys.app/ \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice", "message": "Hello!"}'</code></pre>

<form id="f">
  <div class="form-group">
    <label for="name">Name</label>
    <input id="name" name="name" required placeholder="Your name">
  </div>
  <div class="form-group">
    <label for="message">Message</label>
    <textarea id="message" name="message" required placeholder="Your message"></textarea>
  </div>
  <button type="submit">Send</button>
</form>
<div id="result"></div>

<script>
document.getElementById('f').addEventListener('submit', async function(e) {
  e.preventDefault();
  const btn = this.querySelector('button');
  const res = document.getElementById('result');
  btn.disabled = true;
  res.className = '';
  res.style.display = 'none';
  try {
    const r = await fetch('/', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({
        name: document.getElementById('name').value,
        message: document.getElementById('message').value
      })
    });
    const text = await r.text();
    if (r.ok) {
      res.textContent = 'Message sent!';
      res.className = 'ok';
      document.getElementById('message').value = '';
    } else {
      res.textContent = 'Error: ' + text.trim();
      res.className = 'err';
    }
  } catch (err) {
    res.textContent = 'Error: ' + err.message;
    res.className = 'err';
  } finally {
    btn.disabled = false;
  }
});
</script>
</body>
</html>`

func handler(w http.ResponseWriter, r *http.Request) {
	log.Printf("request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, indexPage)
		return
	}

	if r.Method != http.MethodPost {
		log.Printf("rejected: method %s not allowed", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("rejected: invalid json: %v", err)
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Message == "" {
		log.Printf("rejected: missing name or message")
		http.Error(w, "name and message are required", http.StatusBadRequest)
		return
	}

	log.Printf("processing message from %q: %q", req.Name, req.Message)

	c, err := getOrCreate(req.Name)
	if err != nil {
		log.Printf("setup error for %q: %v", req.Name, err)
		http.Error(w, "failed to set up contact", http.StatusInternalServerError)
		return
	}

	if err := sendMessage(c.conversationID, req.Message); err != nil {
		log.Printf("message error: %v", err)
		http.Error(w, "failed to send message", http.StatusInternalServerError)
		return
	}

	log.Printf("sent message from %q to conversation %d", req.Name, c.conversationID)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func getOrCreate(name string) (*cached, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if c, ok := cache[name]; ok {
		log.Printf("cache hit for %q: contact=%d conversation=%d", name, c.contactID, c.conversationID)
		return c, nil
	}

	log.Printf("cache miss for %q, creating contact", name)
	contactID, sourceID, err := createContact(name)
	if err != nil {
		return nil, fmt.Errorf("create contact: %w", err)
	}
	log.Printf("created contact %d (source=%s) for %q", contactID, sourceID, name)

	convID, err := createConversation(sourceID)
	if err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	log.Printf("created conversation %d for %q", convID, name)

	c := &cached{
		contactID:      contactID,
		sourceID:       sourceID,
		conversationID: convID,
	}
	cache[name] = c
	return c, nil
}

func createContact(name string) (int, string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"inbox_id": cfg.inboxID,
		"name":     name,
	})

	url := fmt.Sprintf("%s/api/v1/accounts/%s/contacts", cfg.baseURL, cfg.accountID)
	resp, err := chatwootRequest(http.MethodPost, url, body)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return 0, "", fmt.Errorf("%s %s", resp.Status, data)
	}

	var result struct {
		Payload struct {
			Contact struct {
				ID             int `json:"id"`
				ContactInboxes []struct {
					SourceID string `json:"source_id"`
				} `json:"contact_inboxes"`
			} `json:"contact"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return 0, "", fmt.Errorf("parse response: %w\nbody: %s", err, data)
	}

	contact := result.Payload.Contact
	if len(contact.ContactInboxes) == 0 {
		return 0, "", fmt.Errorf("no contact_inboxes returned")
	}

	return contact.ID, contact.ContactInboxes[0].SourceID, nil
}

func createConversation(sourceID string) (int, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"source_id": sourceID,
	})

	url := fmt.Sprintf("%s/api/v1/accounts/%s/conversations", cfg.baseURL, cfg.accountID)
	resp, err := chatwootRequest(http.MethodPost, url, body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("%s %s", resp.Status, data)
	}

	var result struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return 0, fmt.Errorf("parse response: %w", err)
	}

	return result.ID, nil
}

func sendMessage(conversationID int, content string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"content":      content,
		"message_type": "incoming",
	})

	url := fmt.Sprintf("%s/api/v1/accounts/%s/conversations/%d/messages", cfg.baseURL, cfg.accountID, conversationID)
	resp, err := chatwootRequest(http.MethodPost, url, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s", resp.Status, data)
	}

	return nil
}

func chatwootRequest(method, url string, body []byte) (*http.Response, error) {
	log.Printf("chatwoot: %s %s body=%s", method, url, body)
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", cfg.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("chatwoot: %s %s error: %v", method, url, err)
		return nil, err
	}
	log.Printf("chatwoot: %s %s -> %s", method, url, resp.Status)
	return resp, nil
}
