package github

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rizznet/internal/logger"
	"rizznet/internal/model"
	"rizznet/internal/publishers"
)

type Publisher struct{}

type githubFileRequest struct {
	Message string `json:"message"`
	Content string `json:"content"` // Base64 encoded content
	Sha     string `json:"sha,omitempty"`
	Branch  string `json:"branch,omitempty"`
}

type githubFileResponse struct {
	Sha string `json:"sha"`
}

func (p *Publisher) Publish(categories []model.Category, config map[string]interface{}) error {
	// 1. Generate Content
	payload, err := publishers.GenerateSubscriptionPayload(categories, config)
	if err != nil {
		return err
	}

	// 2. Parse Config
	token, _ := config["token"].(string)
	owner, _ := config["owner"].(string)
	repo, _ := config["repo"].(string)
	path, _ := config["path"].(string)
	branch, _ := config["branch"].(string)
	msg, _ := config["message"].(string)

	// New: Configurable API URL for Gitea/GitHub Enterprise
	apiBase, _ := config["api_url"].(string)
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	// Normalize URL (remove trailing slash)
	apiBase = strings.TrimRight(apiBase, "/")

	// Determine Timeout (Injected from CMD)
	timeout := 30 * time.Second // Fallback
	if tVal, ok := config["_timeout"]; ok {
		if t, ok := tVal.(time.Duration); ok {
			timeout = t
		}
	}

	if token == "" || owner == "" || repo == "" || path == "" {
		return fmt.Errorf("git publisher requires token, owner, repo, and path")
	}
	if msg == "" {
		msg = "Update proxy subscription [rizznet]"
	}

	// Clean path
	path = strings.TrimPrefix(path, "/")
	
	// Dynamic API URL Construction
	apiURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s", apiBase, owner, repo, path)
	
	// 3. Setup Client with Proxy Support
	client := &http.Client{Timeout: timeout}

	if proxyVal, ok := config["_proxy_url"]; ok {
		if proxyStr, ok := proxyVal.(string); ok && proxyStr != "" {
			if u, err := url.Parse(proxyStr); err == nil {
				client.Transport = &http.Transport{
					Proxy: http.ProxyURL(u),
				}
				logger.Log.Debugf("Git Publisher using proxy: %s", proxyStr)
			}
		}
	}

	// 4. Get existing SHA
	var currentSha string
	
	reqGet, _ := http.NewRequest("GET", apiURL, nil)
	// 'Bearer' is standard for GitHub and modern Gitea (1.14+)
	reqGet.Header.Set("Authorization", "Bearer "+token)
	reqGet.Header.Set("Accept", "application/vnd.github.v3+json")
	
	if branch != "" {
		q := reqGet.URL.Query()
		q.Add("ref", branch)
		reqGet.URL.RawQuery = q.Encode()
	}

	logger.Log.Debugf("Git: Fetching file info from %s", apiURL)
	respGet, err := client.Do(reqGet)
	if err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}
	defer respGet.Body.Close()

	switch respGet.StatusCode {
	case 200:
		var existing githubFileResponse
		if err := json.NewDecoder(respGet.Body).Decode(&existing); err != nil {
			return fmt.Errorf("failed to parse git response: %w", err)
		}
		currentSha = existing.Sha
		logger.Log.Debugf("Git: File exists (SHA: %s), updating...", currentSha)

	case 404:
		currentSha = ""
		logger.Log.Debugf("Git: File not found, creating new...")

	default:
		bodyBytes, _ := io.ReadAll(respGet.Body)
		return fmt.Errorf("git get error (%d): %s", respGet.StatusCode, string(bodyBytes))
	}

	// 5. Upload File (PUT)
	contentEncoded := base64.StdEncoding.EncodeToString([]byte(payload))

	reqBody := githubFileRequest{
		Message: msg,
		Content: contentEncoded,
		Sha:     currentSha,
		Branch:  branch,
	}
	
	jsonBody, _ := json.Marshal(reqBody)
	reqPut, _ := http.NewRequest("PUT", apiURL, bytes.NewBuffer(jsonBody))
	reqPut.Header.Set("Authorization", "Bearer "+token)
	reqPut.Header.Set("Content-Type", "application/json")
	reqPut.Header.Set("Accept", "application/vnd.github.v3+json")

	respPut, err := client.Do(reqPut)
	if err != nil {
		return fmt.Errorf("failed to connect to git host: %w", err)
	}
	defer respPut.Body.Close()

	if respPut.StatusCode < 200 || respPut.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(respPut.Body)
		return fmt.Errorf("git api error (%d): %s", respPut.StatusCode, string(bodyBytes))
	}

	return nil
}

func init() {
	// Registered as 'github' for backward compatibility, 
	// but functions as a generic Git HTTP API publisher.
	publishers.Register("github", func() publishers.Publisher { return &Publisher{} })
}
