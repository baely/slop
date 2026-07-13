package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
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
	aliasID        string
	name           string
}

type Message struct {
	ID          int    `json:"id"`
	Content     string `json:"content"`
	MessageType int    `json:"message_type"`
	CreatedAt   int64  `json:"created_at"`
}

var (
	cfg       config
	cache     = map[string]*cached{} // keyed by name
	aliasByID = map[string]*cached{} // keyed by internal alias
	cacheMu   sync.Mutex
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

	http.HandleFunc("/conversations/", conversationHandler)
	http.HandleFunc("/", handler)
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

const indexPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>hermes</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..96,600;12..96,800&family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;700&display=swap">
<style>
  /* ---------- tokens (copied from house.css) ---------- */
  :root {
    --font-display: 'Bricolage Grotesque', system-ui, sans-serif;
    --font-body: 'Inter', system-ui, sans-serif;
    --font-mono: 'JetBrains Mono', ui-monospace, monospace;

    --accent: #0891b2;
    --accent-deep: #0e7490;
    --accent-ink: #ffffff;

    --r-ctl: 3px;
    --r-card: 4px;
    --sheen: linear-gradient(180deg, rgb(255 255 255 / .12), rgb(0 0 0 / .08));

    --bg: #fafafa;
    --surface: #f1f1f3;
    --surface-2: #e9e9ed;
    --text: #131316;
    --text-2: #55555e;
    --muted: #9b9ba6;
    --line: #e4e4e9;
    --accent-text: #0e7490;
    --accent-soft: #e2f0f4;

    color-scheme: light dark;
  }
  @media (prefers-color-scheme: dark) {
    :root:not([data-theme="light"]) {
      --bg: #0b0b0d;
      --surface: #17171a;
      --surface-2: #212126;
      --text: #f2f2f4;
      --text-2: #a3a3ad;
      --muted: #5b5b66;
      --line: #26262c;
      --accent-text: #3aa8c4;
      --accent-soft: #0c2229;
    }
  }
  :root[data-theme="dark"] {
    --bg: #0b0b0d;
    --surface: #17171a;
    --surface-2: #212126;
    --text: #f2f2f4;
    --text-2: #a3a3ad;
    --muted: #5b5b66;
    --line: #26262c;
    --accent-text: #3aa8c4;
    --accent-soft: #0c2229;
  }

  /* ---------- base ---------- */
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: var(--font-body);
    font-size: 14.5px;
    line-height: 1.55;
    color: var(--text);
    background: var(--bg);
    max-width: 560px;
    margin: 0 auto;
    padding: 44px 20px 76px;
  }
  ::selection { background: var(--accent); color: var(--accent-ink); }

  .wordmark {
    font-family: var(--font-display);
    font-weight: 800;
    letter-spacing: -0.03em;
    line-height: 1;
    font-size: clamp(34px, 9vw, 46px);
    color: var(--text);
  }
  .subtitle { margin-top: 6px; font-size: 13px; color: var(--text-2); }

  /* ---------- code block ---------- */
  pre {
    margin: 24px 0 28px;
    background: var(--surface);
    border-radius: var(--r-card);
    padding: 16px;
    overflow-x: auto;
  }
  pre code, pre { font-family: var(--font-mono); font-size: 12.5px; line-height: 1.65; color: var(--text); }

  /* ---------- form ---------- */
  .field { margin-bottom: 16px; }
  label {
    display: block;
    margin-bottom: 6px;
    font-family: var(--font-body);
    font-size: 12px;
    font-weight: 600;
    color: var(--text-2);
  }
  input, textarea {
    width: 100%;
    font-family: var(--font-body);
    font-size: 14.5px;
    color: var(--text);
    background: var(--surface);
    border: none;
    border-radius: var(--r-ctl);
    padding: 11px 12px;
    min-height: 44px;
    outline: none;
  }
  input::placeholder, textarea::placeholder { color: var(--muted); }
  input:focus, textarea:focus { outline: 2px solid var(--accent); outline-offset: 2px; }
  textarea { resize: vertical; min-height: 100px; }

  button {
    display: block;
    width: 100%;
    margin-top: 8px;
    font-family: var(--font-body);
    font-size: 14px;
    font-weight: 600;
    color: var(--accent-ink);
    background-color: var(--accent);
    background-image: var(--sheen);
    border: none;
    border-radius: var(--r-ctl);
    padding: 12px 16px;
    min-height: 44px;
    cursor: pointer;
  }
  button:hover:not(:disabled) { background-color: var(--accent-deep); }
  button:disabled { opacity: 0.5; cursor: not-allowed; }

  /* ---------- result ---------- */
  #result {
    margin-top: 16px;
    padding: 10px 12px;
    border-radius: var(--r-ctl);
    font-size: 13px;
    display: none;
  }
  #result.ok { display: block; background: var(--accent-soft); color: var(--accent-text); }
  #result.ok a { color: var(--accent-text); }
  #result.err { display: block; background: var(--surface); color: #b91c1c; }

  /* ---------- signature ---------- */
  .b-glyph {
    position: fixed;
    bottom: 10px;
    right: 10px;
    z-index: 90;
    font-family: var(--font-display);
    font-weight: 800;
    font-size: 13px;
    line-height: 1;
    color: var(--text-2);
    background: var(--surface);
    border-radius: var(--r-ctl);
    padding: 6px 8px;
    text-decoration: none;
  }
  .b-glyph:hover { color: var(--accent-text); }
</style>
</head>
<body>
<h1 class="wordmark">hermes</h1>
<p class="subtitle">Message API</p>

<pre><code>curl -X POST https://hermes.baileys.app/ \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice", "message": "Hello!"}'</code></pre>

<form id="f">
  <div class="field">
    <label for="name">Name</label>
    <input id="name" name="name" required placeholder="Your name">
  </div>
  <div class="field">
    <label for="message">Message</label>
    <textarea id="message" name="message" required placeholder="Your message"></textarea>
  </div>
  <button type="submit">Send Message</button>
</form>
<div id="result"></div>

<a class="b-glyph" href="https://index.baileys.app" title="A Bailey App">b.</a>

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
    if (r.ok) {
      const data = await r.json();
      res.innerHTML = 'Sent. <a href="' + data.url + '">' + data.url + '</a>';
      res.className = 'ok';
      document.getElementById('message').value = '';
    } else {
      const text = await r.text();
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

func conversationURL(r *http.Request, alias string) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return scheme + "://" + r.Host + "/conversations/" + alias
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Printf("request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

	setCORSHeaders(w)

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

	log.Printf("sent message from %q to conversation %d (alias %s)", req.Name, c.conversationID, c.aliasID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"url": conversationURL(r, c.aliasID),
	})
}

func conversationHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

	setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	alias := strings.TrimPrefix(r.URL.Path, "/conversations/")
	alias = strings.TrimSuffix(alias, "/")
	if alias == "" {
		http.Error(w, "conversation id required", http.StatusBadRequest)
		return
	}

	cacheMu.Lock()
	c, ok := aliasByID[alias]
	cacheMu.Unlock()

	if !ok {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		messages, err := getMessages(c.conversationID)
		if err != nil {
			log.Printf("get messages error: %v", err)
			http.Error(w, "failed to get messages", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":     c.name,
			"messages": messages,
			"url":      conversationURL(r, alias),
		})

	case http.MethodPost:
		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("rejected: invalid json: %v", err)
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if req.Message == "" {
			http.Error(w, "message is required", http.StatusBadRequest)
			return
		}

		if err := sendMessage(c.conversationID, req.Message); err != nil {
			log.Printf("message error: %v", err)
			http.Error(w, "failed to send message", http.StatusInternalServerError)
			return
		}

		log.Printf("sent message to conversation %d (alias %s) as %q", c.conversationID, alias, c.name)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"url": conversationURL(r, alias),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func generateAlias() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func getOrCreate(name string) (*cached, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if c, ok := cache[name]; ok {
		log.Printf("cache hit for %q: contact=%d conversation=%d alias=%s", name, c.contactID, c.conversationID, c.aliasID)
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

	alias, err := generateAlias()
	if err != nil {
		return nil, fmt.Errorf("generate alias: %w", err)
	}

	c := &cached{
		contactID:      contactID,
		sourceID:       sourceID,
		conversationID: convID,
		aliasID:        alias,
		name:           name,
	}
	cache[name] = c
	aliasByID[alias] = c
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

func getMessages(conversationID int) ([]Message, error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/conversations/%d/messages", cfg.baseURL, cfg.accountID, conversationID)
	resp, err := chatwootRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s %s", resp.Status, data)
	}

	var result struct {
		Payload struct {
			Messages []Message `json:"messages"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w\nbody: %s", err, data)
	}

	return result.Payload.Messages, nil
}

func chatwootRequest(method, url string, body []byte) (*http.Response, error) {
	log.Printf("chatwoot: %s %s", method, url)
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("api_access_token", cfg.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("chatwoot: %s %s error: %v", method, url, err)
		return nil, err
	}
	log.Printf("chatwoot: %s %s -> %s", method, url, resp.Status)
	return resp, nil
}
