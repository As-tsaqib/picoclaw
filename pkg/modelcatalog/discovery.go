package modelcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/auth"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/providers"
)

var ErrUnsupported = errors.New("model discovery unsupported")

type Model struct {
	ID      string
	OwnedBy string
}

// Fetch returns the models reported by the configured provider. It never
// performs inference and never persists credentials or discovered models.
func Fetch(ctx context.Context, mc *config.ModelConfig) ([]Model, error) {
	if mc == nil {
		return nil, fmt.Errorf("model config is required")
	}
	provider, _ := providers.ExtractProtocol(mc)
	provider = providers.NormalizeProvider(provider)
	if !providers.IsModelProviderFetchable(provider) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, provider)
	}
	apiBase := strings.TrimRight(strings.TrimSpace(mc.APIBase), "/")
	if apiBase == "" && provider != "antigravity" {
		apiBase = strings.TrimRight(providers.DefaultAPIBaseForProtocol(provider), "/")
	}
	if apiBase == "" && provider != "antigravity" {
		return nil, fmt.Errorf("no API base for provider %q", provider)
	}

	client, err := discoveryHTTPClient(mc)
	if err != nil {
		return nil, err
	}
	if provider == "antigravity" {
		return fetchAntigravity(ctx, client, mc)
	}

	var models []Model
	switch provider {
	case "ollama":
		root := strings.TrimSuffix(apiBase, "/v1")
		models, err = fetchOllama(ctx, client, mc, strings.TrimRight(root, "/")+"/api/tags")
	case "nearai":
		models, err = fetchNearAI(ctx, client, mc, apiBase+"/model/list")
	default:
		models, err = fetchOpenAICompatible(ctx, client, mc, apiBase+"/models")
	}
	if err != nil {
		return nil, err
	}
	return normalize(models), nil
}

func discoveryHTTPClient(mc *config.ModelConfig) (*http.Client, error) {
	if mc == nil {
		return nil, fmt.Errorf("model config is required")
	}
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is unavailable")
	}
	transport := baseTransport.Clone()
	proxy := strings.TrimSpace(mc.Proxy)
	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid model discovery proxy: %w", err)
		}
		switch strings.ToLower(proxyURL.Scheme) {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, fmt.Errorf("unsupported model discovery proxy scheme %q", proxyURL.Scheme)
		}
		if strings.TrimSpace(proxyURL.Host) == "" {
			return nil, fmt.Errorf("invalid model discovery proxy: host is required")
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}, nil
}

func newDiscoveryRequest(
	ctx context.Context,
	mc *config.ModelConfig,
	requestURL string,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range mc.CustomHeaders {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	if userAgent := strings.TrimSpace(mc.UserAgent); userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	return req, nil
}

func normalize(models []Model) []Model {
	byID := make(map[string]Model, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		model.OwnedBy = strings.TrimSpace(model.OwnedBy)
		if model.ID == "" {
			continue
		}
		if _, exists := byID[model.ID]; !exists {
			byID[model.ID] = model
		}
	}
	out := make([]Model, 0, len(byID))
	for _, model := range byID {
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID)
	})
	return out
}

func fetchOpenAICompatible(
	ctx context.Context,
	client *http.Client,
	mc *config.ModelConfig,
	requestURL string,
) ([]Model, error) {
	req, err := newDiscoveryRequest(ctx, mc, requestURL)
	if err != nil {
		return nil, err
	}
	if apiKey := strings.TrimSpace(mc.APIKey()); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	type item struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
	}
	var envelope struct {
		Data []item `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Data != nil {
		out := make([]Model, 0, len(envelope.Data))
		for _, m := range envelope.Data {
			out = append(out, Model{ID: m.ID, OwnedBy: m.OwnedBy})
		}
		return out, nil
	}
	var array []item
	if err := json.Unmarshal(body, &array); err == nil {
		out := make([]Model, 0, len(array))
		for _, m := range array {
			out = append(out, Model{ID: m.ID, OwnedBy: m.OwnedBy})
		}
		return out, nil
	}
	return nil, fmt.Errorf("unrecognized model-list response")
}

func fetchOllama(
	ctx context.Context,
	client *http.Client,
	mc *config.ModelConfig,
	requestURL string,
) ([]Model, error) {
	req, err := newDiscoveryRequest(ctx, mc, requestURL)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}
	var parsed struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&parsed); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(parsed.Models))
	for _, item := range parsed.Models {
		id := strings.TrimSpace(item.Name)
		if id == "" {
			id = strings.TrimSpace(item.Model)
		}
		out = append(out, Model{ID: id})
	}
	return out, nil
}

func fetchNearAI(
	ctx context.Context,
	client *http.Client,
	mc *config.ModelConfig,
	requestURL string,
) ([]Model, error) {
	req, err := newDiscoveryRequest(ctx, mc, requestURL)
	if err != nil {
		return nil, err
	}
	if apiKey := strings.TrimSpace(mc.APIKey()); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nearai returned status %d", resp.StatusCode)
	}
	var parsed struct {
		Models []struct {
			ModelID  string `json:"modelId"`
			OwnedBy  string `json:"ownedBy"`
			Metadata struct {
				OwnedBy string `json:"ownedBy"`
			} `json:"metadata"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&parsed); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(parsed.Models))
	for _, item := range parsed.Models {
		ownedBy := strings.TrimSpace(item.OwnedBy)
		if ownedBy == "" {
			ownedBy = strings.TrimSpace(item.Metadata.OwnedBy)
		}
		out = append(out, Model{ID: item.ModelID, OwnedBy: ownedBy})
	}
	return out, nil
}

func fetchAntigravity(ctx context.Context, client *http.Client, mc *config.ModelConfig) ([]Model, error) {
	cred, err := auth.GetCredential("google-antigravity")
	if err != nil {
		return nil, fmt.Errorf("loading antigravity credentials: %w", err)
	}
	if cred == nil {
		return nil, fmt.Errorf("not logged in to antigravity")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if cred.NeedsRefresh() && cred.RefreshToken != "" {
		refreshed, refreshErr := auth.RefreshAccessTokenContext(ctx, cred, auth.GoogleAntigravityOAuthConfig())
		if refreshErr == nil {
			refreshed.Email = cred.Email
			if refreshed.ProjectID == "" {
				refreshed.ProjectID = cred.ProjectID
			}
			_ = auth.SetCredential("google-antigravity", refreshed)
			cred = refreshed
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if strings.TrimSpace(cred.ProjectID) == "" {
		return nil, fmt.Errorf("antigravity project ID is unavailable")
	}
	items, err := providers.FetchAntigravityModelsWithClientContext(
		ctx,
		client,
		cred.AccessToken,
		cred.ProjectID,
		mc.CustomHeaders,
		mc.UserAgent,
	)
	if err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(items))
	for _, item := range items {
		ownedBy := "google"
		if item.IsExhausted {
			ownedBy = "google (quota exhausted)"
		}
		out = append(out, Model{ID: item.ID, OwnedBy: ownedBy})
	}
	return normalize(out), nil
}
