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

	apiBase, _ := config["api_url"].(string)
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	apiBase = strings.TrimRight(apiBase, "/")

	// Determine Timeout & Retries
	timeout := 30 * time.Second
	if tVal, ok := config["_timeout"]; ok {
		if t, ok := tVal.(time.Duration); ok {
			timeout = t
		}
	}

	retries := 0
	if rVal, ok := config["_retries"]; ok {
		if r, ok := rVal.(int); ok {
			retries = r
		}
	}

	if token == "" || owner == "" || repo == "" || path == "" {
		return fmt.Errorf("git publisher requires token, owner, repo, and path")
	}
	if msg == "" {
		msg = "Update proxy subscription [rizznet]"
	}

	path = strings.TrimPrefix(path, "/")
	apiURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s", apiBase, owner, repo, path)
	
	// 3. Setup Client
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

	// 4. Get existing SHA (With Retries)
	var currentSha string
	var respGet *http.Response
	
	for i := 0; i <= retries; i++ {
		reqGet, _ := http.NewRequest("GET", apiURL, nil)
		reqGet.Header.Set("Authorization", "Bearer "+token)
		reqGet.Header.Set("Accept", "application/vnd.github.v3+json")
		
		if branch != "" {
			q := reqGet.URL.Query()
			q.Add("ref", branch)
			reqGet.URL.RawQuery = q.Encode()
		}

		logger.Log.Debugf("Git: Fetching file info (Attempt %d/%d)", i+1, retries+1)
		respGet, err = client.Do(reqGet)
		if err == nil && (respGet.StatusCode == 200 || respGet.StatusCode == 404) {
			break
		}
		
		if err == nil {
			// Server error or unexpected status, retry
			respGet.Body.Close()
			err = fmt.Errorf("status %d", respGet.StatusCode)
		}

		if i < retries {
			time.Sleep(1 * time.Second)
		}
	}

	if err != nil {
		return fmt.Errorf("git fetch failed after retries: %w", err)
	}
	defer respGet.Body.Close()

	if respGet.StatusCode == 200 {
		var existing githubFileResponse
		if err := json.NewDecoder(respGet.Body).Decode(&existing); err != nil {
			return fmt.Errorf("failed to parse git response: %w", err)
		}
		currentSha = existing.Sha
		logger.Log.Debugf("Git: File exists (SHA: %s), updating...", currentSha)
	} else if respGet.StatusCode == 404 {
		currentSha = ""
		logger.Log.Debugf("Git: File not found, creating new...")
	} else {
		// Should be caught by loop, but safe fallback
		return fmt.Errorf("git unexpected status: %d", respGet.StatusCode)
	}

	// 5. Upload File (PUT) (With Retries)
	contentEncoded := base64.StdEncoding.EncodeToString([]byte(payload))
	reqBody := githubFileRequest{
		Message: msg,
		Content: contentEncoded,
		Sha:     currentSha,
		Branch:  branch,
	}
	jsonBody, _ := json.Marshal(reqBody)

	var respPut *http.Response
	for i := 0; i <= retries; i++ {
		reqPut, _ := http.NewRequest("PUT", apiURL, bytes.NewBuffer(jsonBody))
		reqPut.Header.Set("Authorization", "Bearer "+token)
		reqPut.Header.Set("Content-Type", "application/json")
		reqPut.Header.Set("Accept", "application/vnd.github.v3+json")

		logger.Log.Debugf("Git: Uploading file (Attempt %d/%d)", i+1, retries+1)
		respPut, err = client.Do(reqPut)
		if err == nil && (respPut.StatusCode >= 200 && respPut.StatusCode < 300) {
			break
		}

		if err == nil {
			bodyBytes, _ := io.ReadAll(respPut.Body)
			respPut.Body.Close()
			err = fmt.Errorf("status %d: %s", respPut.StatusCode, string(bodyBytes))
		}

		if i < retries {
			time.Sleep(1 * time.Second)
		}
	}

	if err != nil {
		return fmt.Errorf("git upload failed after retries: %w", err)
	}
	defer respPut.Body.Close()

	return nil
}

func init() {
	publishers.Register("github", func() publishers.Publisher { return &Publisher{} })
}
