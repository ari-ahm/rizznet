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

	if token == "" || owner == "" || repo == "" || path == "" {
		return fmt.Errorf("github publisher requires token, owner, repo, and path")
	}
	if msg == "" {
		msg = "Update proxy subscription [rizznet]"
	}

	// Clean path
	path = strings.TrimPrefix(path, "/")
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)
	
	// 3. Setup Client with Proxy Support
	client := &http.Client{Timeout: 30 * time.Second}

	if proxyVal, ok := config["_proxy_url"]; ok {
		if proxyStr, ok := proxyVal.(string); ok && proxyStr != "" {
			if u, err := url.Parse(proxyStr); err == nil {
				client.Transport = &http.Transport{
					Proxy: http.ProxyURL(u),
				}
				logger.Log.Debugf("GitHub Publisher using proxy: %s", proxyStr)
			}
		}
	}

	// 4. Get existing SHA
	var currentSha string
	
	reqGet, _ := http.NewRequest("GET", apiURL, nil)
	reqGet.Header.Set("Authorization", "Bearer "+token)
	reqGet.Header.Set("Accept", "application/vnd.github.v3+json")
	
	if branch != "" {
		q := reqGet.URL.Query()
		q.Add("ref", branch)
		reqGet.URL.RawQuery = q.Encode()
	}

	logger.Log.Debugf("GitHub: Fetching file info from %s", apiURL)
	respGet, err := client.Do(reqGet)
	if err != nil {
		return fmt.Errorf("github fetch failed: %w", err)
	}
	defer respGet.Body.Close()

	switch respGet.StatusCode {
	case 200:
		var existing githubFileResponse
		if err := json.NewDecoder(respGet.Body).Decode(&existing); err != nil {
			return fmt.Errorf("failed to parse github response: %w", err)
		}
		currentSha = existing.Sha
		logger.Log.Debugf("GitHub: File exists (SHA: %s), updating...", currentSha)

	case 404:
		currentSha = ""
		logger.Log.Debugf("GitHub: File not found, creating new...")

	default:
		bodyBytes, _ := io.ReadAll(respGet.Body)
		return fmt.Errorf("github get error (%d): %s", respGet.StatusCode, string(bodyBytes))
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
		return fmt.Errorf("failed to connect to github: %w", err)
	}
	defer respPut.Body.Close()

	if respPut.StatusCode < 200 || respPut.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(respPut.Body)
		return fmt.Errorf("github api error (%d): %s", respPut.StatusCode, string(bodyBytes))
	}

	return nil
}

func init() {
	publishers.Register("github", func() publishers.Publisher { return &Publisher{} })
}
