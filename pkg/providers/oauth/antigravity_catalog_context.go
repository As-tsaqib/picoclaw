package oauthprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FetchAntigravityModelsContext is the cancellable counterpart of
// FetchAntigravityModels. Model catalog callers use this path so a command-level
// timeout cancels the underlying HTTP request instead of only timing the caller.
func FetchAntigravityModelsContext(
	ctx context.Context,
	accessToken, projectID string,
) ([]AntigravityModelInfo, error) {
	reqBody, _ := json.Marshal(map[string]any{
		"project": projectID,
	})

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		antigravityBaseURL+"/v1internal:fetchAvailableModels",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravityUserAgent)
	req.Header.Set("X-Goog-Api-Client", antigravityXGoogClient)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading fetchAvailableModels response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"fetchAvailableModels failed (HTTP %d): %s",
			resp.StatusCode,
			truncateString(string(body), 200),
		)
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

	models := make([]AntigravityModelInfo, 0, len(result.Models)+2)
	for id, info := range result.Models {
		models = append(models, AntigravityModelInfo{
			ID:          id,
			DisplayName: info.DisplayName,
			IsExhausted: info.QuotaInfo.IsExhausted,
		})
	}

	hasFlashPreview := false
	hasFlash := false
	for _, model := range models {
		switch model.ID {
		case "gemini-3-flash-preview":
			hasFlashPreview = true
		case "gemini-3-flash":
			hasFlash = true
		}
	}
	if !hasFlashPreview {
		models = append(models, AntigravityModelInfo{
			ID:          "gemini-3-flash-preview",
			DisplayName: "Gemini 3 Flash (Preview)",
		})
	}
	if !hasFlash {
		models = append(models, AntigravityModelInfo{
			ID:          "gemini-3-flash",
			DisplayName: "Gemini 3 Flash",
		})
	}

	return models, nil
}
