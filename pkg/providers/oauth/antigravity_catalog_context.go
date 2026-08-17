package oauthprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AntigravityHTTPError preserves the HTTP status needed for bounded auth
// recovery without retaining an upstream response body that could contain
// credentials or request metadata.
type AntigravityHTTPError struct {
	Operation  string
	StatusCode int
}

func (e *AntigravityHTTPError) Error() string {
	if e == nil {
		return "antigravity request failed"
	}
	return fmt.Sprintf("%s failed (HTTP %d)", e.Operation, e.StatusCode)
}

func IsAntigravityUnauthorized(err error) bool {
	var httpErr *AntigravityHTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnauthorized
}

// FetchAntigravityModelsContext is the cancellable counterpart of
// FetchAntigravityModels. Model catalog callers use this path so a command-level
// timeout cancels the underlying HTTP request instead of only timing the caller.
func FetchAntigravityModelsContext(
	ctx context.Context,
	accessToken, projectID string,
) ([]AntigravityModelInfo, error) {
	return FetchAntigravityModelsWithClientContext(
		ctx,
		&http.Client{Timeout: 15 * time.Second},
		accessToken,
		projectID,
		nil,
		"",
	)
}

// FetchAntigravityModelsWithClientContext applies the configured discovery
// transport and headers while keeping provider authentication authoritative.
func FetchAntigravityModelsWithClientContext(
	ctx context.Context,
	client *http.Client,
	accessToken, projectID string,
	customHeaders map[string]string,
	userAgent string,
) ([]AntigravityModelInfo, error) {
	return FetchAntigravityModelsAtBaseURLWithClientContext(
		ctx,
		client,
		antigravityBaseURL,
		accessToken,
		projectID,
		customHeaders,
		userAgent,
	)
}

// FetchAntigravityModelsAtBaseURLWithClientContext is the testable/configurable
// catalog primitive. An empty baseURL uses the production Antigravity endpoint.
func FetchAntigravityModelsAtBaseURLWithClientContext(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	accessToken, projectID string,
	customHeaders map[string]string,
	userAgent string,
) ([]AntigravityModelInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	} else if client.Timeout <= 0 {
		clone := *client
		clone.Timeout = 15 * time.Second
		client = &clone
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = antigravityBaseURL
	}

	reqBody, _ := json.Marshal(map[string]any{
		"project": projectID,
	})

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/v1internal:fetchAvailableModels",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return nil, err
	}
	for key, value := range customHeaders {
		key = strings.TrimSpace(key)
		if key != "" {
			req.Header.Set(key, value)
		}
	}
	// Authentication and protocol-critical headers are authoritative even when
	// custom headers contain conflicting values.
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(userAgent) == "" {
		userAgent = antigravityUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Goog-Api-Client", antigravityXGoogClient)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("reading fetchAvailableModels response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &AntigravityHTTPError{Operation: "fetchAvailableModels", StatusCode: resp.StatusCode}
	}

	var result struct {
		Models map[string]struct {
			DisplayName string `json:"displayName"`
			QuotaInfo   struct {
				RemainingFraction any    `json:"remainingFraction"`
				ResetTime         string `json:"resetTime"`
				IsExhausted       bool   `json:"isExhausted"`
			} `json:"quotaInfo"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing models response: %w", err)
	}

	models := make([]AntigravityModelInfo, 0, len(result.Models))
	for id, info := range result.Models {
		models = append(models, AntigravityModelInfo{
			ID:          id,
			DisplayName: info.DisplayName,
			IsExhausted: info.QuotaInfo.IsExhausted,
		})
	}
	return models, nil
}
