package oauthprovider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/auth"
	"github.com/As-tsaqib/picoclaw/pkg/logger"
	"github.com/As-tsaqib/picoclaw/pkg/providers/common"
)

var antigravityOAuthConfig = auth.GoogleAntigravityOAuthConfig

const (
	antigravityBaseURL      = "https://cloudcode-pa.googleapis.com"
	antigravityDefaultModel = "gemini-3-flash"
	antigravityUserAgent    = "antigravity"
	antigravityXGoogClient  = "google-cloud-sdk vscode_cloudshelleditor/0.1"
	antigravityVersion      = "1.15.8"
)

const (
	antigravityDailyBaseURL = "https://daily-cloudcode-pa.googleapis.com"
	antigravityRefreshSkew  = 50 * time.Minute
)

// AntigravityProvider implements LLMProvider using Google's Cloud Code Assist (Antigravity) API.
// This provider authenticates via Google OAuth and provides access to models like Claude and Gemini
// through Google's infrastructure.
type AntigravityProvider struct {
	httpClient    *http.Client
	authClient    *http.Client
	baseURL       string
	customHeaders map[string]string
	userAgent     string
}

// NewAntigravityProvider creates a new Antigravity provider using stored auth credentials.
func NewAntigravityProvider() *AntigravityProvider {
	provider, err := NewAntigravityProviderWithConfig("", "", "", 0, nil)
	if err != nil {
		// The default constructor has no user-supplied URL or proxy and therefore
		// cannot fail under normal conditions. Keep the historical no-error API.
		return &AntigravityProvider{
			httpClient: &http.Client{Timeout: 120 * time.Second},
			authClient: &http.Client{Timeout: 15 * time.Second},
			baseURL:    antigravityBaseURL,
		}
	}
	return provider
}

// NewAntigravityProviderWithConfig creates an Antigravity provider that honors
// configured endpoint, proxy, user-agent, timeout, and custom headers. OAuth
// refresh uses the same proxy transport but a shorter bounded timeout.
func NewAntigravityProviderWithConfig(
	apiBase, proxy, userAgent string,
	requestTimeoutSeconds int,
	customHeaders map[string]string,
) (*AntigravityProvider, error) {
	transport, err := antigravityTransport(proxy)
	if err != nil {
		return nil, err
	}
	requestTimeout := 120 * time.Second
	if requestTimeoutSeconds > 0 {
		requestTimeout = time.Duration(requestTimeoutSeconds) * time.Second
	}
	baseURL := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if baseURL == "" {
		baseURL = antigravityBaseURL
	}
	headers := make(map[string]string, len(customHeaders))
	for key, value := range customHeaders {
		headers[key] = value
	}
	return &AntigravityProvider{
		httpClient:    &http.Client{Transport: transport, Timeout: requestTimeout},
		authClient:    &http.Client{Transport: transport, Timeout: 15 * time.Second},
		baseURL:       baseURL,
		customHeaders: headers,
		userAgent:     strings.TrimSpace(userAgent),
	}, nil
}

func antigravityTransport(proxy string) (*http.Transport, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is unavailable")
	}
	transport := base.Clone()
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return transport, nil
	}
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return nil, fmt.Errorf("invalid antigravity proxy: %w", err)
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("unsupported antigravity proxy scheme %q", proxyURL.Scheme)
	}
	if strings.TrimSpace(proxyURL.Host) == "" {
		return nil, fmt.Errorf("invalid antigravity proxy: host is required")
	}
	transport.Proxy = http.ProxyURL(proxyURL)
	return transport, nil
}

// Chat implements LLMProvider.Chat using the Cloud Code Assist v1internal API.
// The v1internal endpoint wraps the standard Gemini request in an envelope with
// project, model, request, requestType, userAgent, and requestId fields.
func (p *AntigravityProvider) Chat(
	ctx context.Context,
	messages []Message,
	tools []ToolDefinition,
	model string,
	options map[string]any,
) (*LLMResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cred, err := p.credential(ctx)
	if err != nil {
		return nil, fmt.Errorf("antigravity auth: %w", err)
	}

	if model == "" || model == "antigravity" || model == "google-antigravity" {
		model = antigravityDefaultModel
	}
	model = strings.TrimPrefix(model, "google-antigravity/")
	model = strings.TrimPrefix(model, "antigravity/")

	logger.DebugCF("provider.antigravity", "Starting chat", map[string]any{
		"model":     model,
		"project":   cred.ProjectID,
		"requestId": fmt.Sprintf("agent-%d-%s", time.Now().UnixMilli(), randomString(9)),
	})

	innerRequest := p.buildRequest(messages, tools, model, options)
	envelope := map[string]any{
		"project":     cred.ProjectID,
		"model":       model,
		"request":     innerRequest,
		"requestType": "agent",
		"userAgent":   antigravityUserAgent,
		"requestId":   fmt.Sprintf("agent-%d-%s", time.Now().UnixMilli(), randomString(9)),
	}
	bodyBytes, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	statusCode, respBody, err := p.doChatRequest(ctx, cred.AccessToken, bodyBytes)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusUnauthorized {
		recovered, refreshErr := auth.RefreshCredentialAfterUnauthorizedContext(
			ctx,
			"google-antigravity",
			cred,
			antigravityOAuthConfig(),
			p.authHTTPClient(),
		)
		if refreshErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("antigravity auth recovery: %w", refreshErr)
		}
		cred = recovered
		statusCode, respBody, err = p.doChatRequest(ctx, cred.AccessToken, bodyBytes)
		if err != nil {
			return nil, err
		}
	}

	if statusCode != http.StatusOK {
		logger.ErrorCF("provider.antigravity", "API call failed", map[string]any{
			"status_code": statusCode,
			"model":       model,
		})
		return nil, p.parseAntigravityError(statusCode, respBody)
	}

	llmResp, err := p.parseSSEResponse(string(respBody))
	if err != nil {
		return nil, err
	}
	if llmResp.Content == "" && len(llmResp.ToolCalls) == 0 {
		return nil, fmt.Errorf(
			"antigravity: model returned an empty response (this model might be invalid or restricted)",
		)
	}
	return llmResp, nil
}

func (p *AntigravityProvider) doChatRequest(
	ctx context.Context,
	accessToken string,
	body []byte,
) (int, []byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	apiURL := strings.TrimRight(p.apiBaseURL(), "/") + "/v1internal:streamGenerateContent?alt=sse"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("creating request: %w", err)
	}
	for key, value := range p.customHeaders {
		key = strings.TrimSpace(key)
		if key != "" {
			req.Header.Set(key, value)
		}
	}
	clientMetadata, _ := json.Marshal(map[string]string{"ideType": "ANTIGRAVITY"})
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	userAgent := strings.TrimSpace(p.userAgent)
	if userAgent == "" {
		userAgent = fmt.Sprintf("antigravity/%s linux/amd64", antigravityVersion)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Goog-Api-Client", antigravityXGoogClient)
	req.Header.Set("Client-Metadata", string(clientMetadata))

	resp, err := p.requestHTTPClient().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("antigravity API call: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("reading response: %w", err)
	}
	return resp.StatusCode, respBody, nil
}

func (p *AntigravityProvider) requestHTTPClient() *http.Client {
	if p != nil && p.httpClient != nil {
		return p.httpClient
	}
	return &http.Client{Timeout: 120 * time.Second}
}

func (p *AntigravityProvider) authHTTPClient() *http.Client {
	if p != nil && p.authClient != nil {
		return p.authClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (p *AntigravityProvider) apiBaseURL() string {
	if p != nil && strings.TrimSpace(p.baseURL) != "" {
		return p.baseURL
	}
	return antigravityBaseURL
}

// GetDefaultModel returns the default model identifier.
func (p *AntigravityProvider) GetDefaultModel() string {
	return antigravityDefaultModel
}

// --- Request building ---

type antigravityRequest struct {
	Contents     []antigravityContent     `json:"contents"`
	Tools        []antigravityTool        `json:"tools,omitempty"`
	SystemPrompt *antigravitySystemPrompt `json:"systemInstruction,omitempty"`
	Config       *antigravityGenConfig    `json:"generationConfig,omitempty"`
}

type antigravityContent struct {
	Role  string            `json:"role"`
	Parts []antigravityPart `json:"parts"`
}

type antigravityPart struct {
	Text                  string                       `json:"text,omitempty"`
	ThoughtSignature      string                       `json:"thoughtSignature,omitempty"`
	ThoughtSignatureSnake string                       `json:"thought_signature,omitempty"`
	FunctionCall          *antigravityFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse      *antigravityFunctionResponse `json:"functionResponse,omitempty"`
}

type antigravityFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type antigravityFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type antigravityTool struct {
	FunctionDeclarations []antigravityFuncDecl `json:"functionDeclarations"`
}

type antigravityFuncDecl struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type antigravitySystemPrompt struct {
	Parts []antigravityPart `json:"parts"`
}

type antigravityGenConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
}

func (p *AntigravityProvider) buildRequest(
	messages []Message,
	tools []ToolDefinition,
	model string,
	options map[string]any,
) antigravityRequest {
	req := antigravityRequest{}
	toolCallNames := make(map[string]string)

	// Build contents from messages
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			req.SystemPrompt = &antigravitySystemPrompt{
				Parts: []antigravityPart{{Text: msg.Content}},
			}
		case "user":
			if msg.ToolCallID != "" {
				toolName := common.ResolveToolResponseName(msg.ToolCallID, toolCallNames)
				// Tool result
				req.Contents = append(req.Contents, antigravityContent{
					Role: "user",
					Parts: []antigravityPart{{
						FunctionResponse: &antigravityFunctionResponse{
							Name: toolName,
							Response: map[string]any{
								"result": msg.Content,
							},
						},
					}},
				})
			} else {
				req.Contents = append(req.Contents, antigravityContent{
					Role:  "user",
					Parts: []antigravityPart{{Text: msg.Content}},
				})
			}
		case "assistant":
			content := antigravityContent{
				Role: "model",
			}
			if msg.Content != "" {
				content.Parts = append(content.Parts, antigravityPart{Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				toolName, toolArgs, thoughtSignature := common.NormalizeStoredToolCall(tc)
				if toolName == "" {
					logger.WarnCF(
						"provider.antigravity",
						"Skipping tool call with empty name in history",
						map[string]any{
							"tool_call_id": tc.ID,
						},
					)
					continue
				}
				if tc.ID != "" {
					toolCallNames[tc.ID] = toolName
				}
				content.Parts = append(content.Parts, antigravityPart{
					ThoughtSignature:      thoughtSignature,
					ThoughtSignatureSnake: thoughtSignature,
					FunctionCall: &antigravityFunctionCall{
						Name: toolName,
						Args: toolArgs,
					},
				})
			}
			if len(content.Parts) > 0 {
				req.Contents = append(req.Contents, content)
			}
		case "tool":
			toolName := common.ResolveToolResponseName(msg.ToolCallID, toolCallNames)
			req.Contents = append(req.Contents, antigravityContent{
				Role: "user",
				Parts: []antigravityPart{{
					FunctionResponse: &antigravityFunctionResponse{
						Name: toolName,
						Response: map[string]any{
							"result": msg.Content,
						},
					},
				}},
			})
		}
	}

	// Build tools
	if len(tools) > 0 {
		var funcDecls []antigravityFuncDecl
		for _, t := range tools {
			if t.Type != "function" {
				continue
			}
			funcDecls = append(funcDecls, antigravityFuncDecl{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			})
		}
		if len(funcDecls) > 0 {
			req.Tools = []antigravityTool{{FunctionDeclarations: funcDecls}}
		}
	}

	// Generation config
	config := &antigravityGenConfig{}
	if val, ok := options["max_tokens"]; ok {
		if maxTokens, ok := val.(int); ok && maxTokens > 0 {
			config.MaxOutputTokens = maxTokens
		} else if maxTokens, ok := val.(float64); ok && maxTokens > 0 {
			config.MaxOutputTokens = int(maxTokens)
		}
	}
	if temp, ok := options["temperature"].(float64); ok {
		config.Temperature = temp
	}
	if config.MaxOutputTokens > 0 || config.Temperature > 0 {
		req.Config = config
	}

	return req
}

// --- Response parsing ---

type antigravityJSONResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text                  string                   `json:"text,omitempty"`
				Thought               bool                     `json:"thought,omitempty"`
				ThoughtSignature      string                   `json:"thoughtSignature,omitempty"`
				ThoughtSignatureSnake string                   `json:"thought_signature,omitempty"`
				FunctionCall          *antigravityFunctionCall `json:"functionCall,omitempty"`
			} `json:"parts"`
			Role string `json:"role"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func (p *AntigravityProvider) parseSSEResponse(body string) (*LLMResponse, error) {
	var contentParts []string
	var reasoningParts []string
	var toolCalls []ToolCall
	var usage *UsageInfo
	var finishReason string

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		// v1internal SSE wraps the Gemini response in a "response" field
		var sseChunk struct {
			Response antigravityJSONResponse `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &sseChunk); err != nil {
			continue
		}
		resp := sseChunk.Response

		for _, candidate := range resp.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					if part.Thought {
						reasoningParts = append(reasoningParts, part.Text)
					} else {
						contentParts = append(contentParts, part.Text)
					}
				}
				if part.FunctionCall != nil {
					argumentsJSON, _ := json.Marshal(part.FunctionCall.Args)
					toolCalls = append(toolCalls, ToolCall{
						ID:        fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, time.Now().UnixNano()),
						Name:      part.FunctionCall.Name,
						Arguments: part.FunctionCall.Args,
						Function: &FunctionCall{
							Name:      part.FunctionCall.Name,
							Arguments: string(argumentsJSON),
							ThoughtSignature: extractPartThoughtSignature(
								part.ThoughtSignature,
								part.ThoughtSignatureSnake,
							),
						},
					})
				}
			}
			if candidate.FinishReason != "" {
				finishReason = candidate.FinishReason
			}
		}

		if resp.UsageMetadata.TotalTokenCount > 0 {
			usage = &UsageInfo{
				PromptTokens:     resp.UsageMetadata.PromptTokenCount,
				CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      resp.UsageMetadata.TotalTokenCount,
			}
		}
	}

	mappedFinish := "stop"
	if len(toolCalls) > 0 {
		mappedFinish = "tool_calls"
	}
	if finishReason == "MAX_TOKENS" {
		mappedFinish = "length"
	}

	return &LLMResponse{
		Content:          strings.Join(contentParts, ""),
		ReasoningContent: strings.Join(reasoningParts, ""),
		ToolCalls:        toolCalls,
		FinishReason:     mappedFinish,
		Usage:            usage,
	}, nil
}

func extractPartThoughtSignature(thoughtSignature string, thoughtSignatureSnake string) string {
	if thoughtSignature != "" {
		return thoughtSignature
	}
	if thoughtSignatureSnake != "" {
		return thoughtSignatureSnake
	}
	return ""
}

// --- Credential handling ---

func (p *AntigravityProvider) credential(ctx context.Context) (*auth.AuthCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cred, err := auth.GetCredential("google-antigravity")
	if err != nil {
		return nil, fmt.Errorf("loading auth credentials: %w", err)
	}
	if cred == nil {
		return nil, fmt.Errorf(
			"no credentials for google-antigravity. Run: picoclaw auth login --provider google-antigravity",
		)
	}

	refreshed, refreshErr := auth.EnsureFreshCredentialContext(
		ctx,
		"google-antigravity",
		cred,
		antigravityOAuthConfig(),
		p.authHTTPClient(),
		antigravityRefreshSkew,
	)
	if refreshErr == nil {
		cred = refreshed
	} else {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// Keep a still-valid token usable during a transient refresh outage. A
		// real 401 below will force generation-aware recovery.
		if cred.IsExpired() {
			return nil, fmt.Errorf("refreshing token: %w", refreshErr)
		}
	}
	if cred.IsExpired() {
		return nil, fmt.Errorf(
			"antigravity credentials expired. Run: picoclaw auth login --provider google-antigravity",
		)
	}

	if strings.TrimSpace(cred.ProjectID) == "" {
		projectID, recoveredCred, err := p.fetchProjectIDWithRecovery(ctx, cred)
		if recoveredCred != nil {
			cred = recoveredCred
		}
		if err != nil {
			logger.WarnCF("provider.antigravity", "Could not fetch project ID from loadCodeAssist", map[string]any{
				"error": err.Error(),
			})
			return nil, fmt.Errorf("antigravity: project ID discovery failed: %w", err)
		}
		updated := *cred
		updated.ProjectID = projectID
		stored, replaced, err := auth.UpdateCredentialIfCurrent("google-antigravity", cred, &updated)
		if err != nil {
			return nil, fmt.Errorf("saving antigravity project identity: %w", err)
		}
		if replaced {
			cred = stored
		} else if stored != nil {
			// A concurrent token rotation won. Never overwrite it with metadata
			// derived from the stale generation.
			cred = stored
		}
	}
	if strings.TrimSpace(cred.ProjectID) == "" {
		return nil, fmt.Errorf("antigravity project ID is unavailable")
	}
	return cred, nil
}

// FetchAntigravityProjectID retrieves the Google Cloud project ID from the loadCodeAssist endpoint.
func FetchAntigravityProjectID(accessToken string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return FetchAntigravityProjectIDWithClientContext(
		ctx,
		&http.Client{Timeout: 15 * time.Second},
		antigravityBaseURL,
		accessToken,
		nil,
		"",
	)
}

// FetchAntigravityProjectIDWithClientContext resolves project identity using a
// bounded, cancellable request path. A configured base URL is also used for
// onboarding so test/private endpoints remain self-contained.
func FetchAntigravityProjectIDWithClientContext(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	accessToken string,
	customHeaders map[string]string,
	userAgent string,
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
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
		"metadata": map[string]any{"ideType": "ANTIGRAVITY"},
	})
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/v1internal:loadCodeAssist",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return "", err
	}
	applyAntigravityControlHeaders(req, accessToken, customHeaders, userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("reading loadCodeAssist response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", &AntigravityHTTPError{Operation: "loadCodeAssist", StatusCode: resp.StatusCode}
	}

	var loadResp map[string]any
	if err := json.Unmarshal(body, &loadResp); err != nil {
		return "", fmt.Errorf("decoding loadCodeAssist response: %w", err)
	}
	if projectID := extractCloudAICompanionProject(loadResp); projectID != "" {
		return projectID, nil
	}

	dailyBaseURL := antigravityDailyBaseURL
	if baseURL != antigravityBaseURL {
		dailyBaseURL = baseURL
	}
	return OnboardUserWithClientContext(
		ctx,
		client,
		dailyBaseURL,
		accessToken,
		extractDefaultTierID(loadResp),
		customHeaders,
		userAgent,
	)
}

func applyAntigravityControlHeaders(
	req *http.Request,
	accessToken string,
	customHeaders map[string]string,
	userAgent string,
) {
	for key, value := range customHeaders {
		key = strings.TrimSpace(key)
		if key != "" {
			req.Header.Set(key, value)
		}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(userAgent) == "" {
		userAgent = antigravityUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Goog-Api-Client", antigravityXGoogClient)
}

// extractCloudAICompanionProject extracts the project ID from a loadCodeAssist response.
func extractCloudAICompanionProject(data map[string]any) string {
	if data == nil {
		return ""
	}
	for _, key := range []string{"cloudaicompanionProject", "projectId", "project"} {
		switch value := data[key].(type) {
		case string:
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		case map[string]any:
			if id, ok := value["id"].(string); ok {
				if trimmed := strings.TrimSpace(id); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

// extractDefaultTierID extracts the default tier ID from a loadCodeAssist response.
func extractDefaultTierID(loadResp map[string]any) string {
	if tiers, ok := loadResp["allowedTiers"].([]any); ok {
		for _, rawTier := range tiers {
			tier, ok := rawTier.(map[string]any)
			if !ok {
				continue
			}
			isDefault, ok := tier["isDefault"].(bool)
			if !ok || !isDefault {
				continue
			}
			if id, ok := tier["id"].(string); ok {
				if trimmed := strings.TrimSpace(id); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	if currentTier, ok := loadResp["currentTier"].(map[string]any); ok {
		if id, ok := currentTier["id"].(string); ok {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				return trimmed
			}
		}
	}
	return "free-tier"
}

// OnboardUser attempts to provision a Cloud Code Assist project for a new user
// by polling the onboardUser endpoint until the operation completes.
func OnboardUser(accessToken, tierID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return OnboardUserWithClientContext(
		ctx,
		&http.Client{Timeout: 15 * time.Second},
		antigravityDailyBaseURL,
		accessToken,
		tierID,
		nil,
		"",
	)
}

// OnboardUserWithClientContext polls onboarding while honoring cancellation and
// the shared project-discovery deadline.
func OnboardUserWithClientContext(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	accessToken, tierID string,
	customHeaders map[string]string,
	userAgent string,
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = antigravityDailyBaseURL
	}
	logger.DebugCF("provider.antigravity", "Onboarding user", map[string]any{"tier_id": tierID})

	reqBody, _ := json.Marshal(map[string]any{
		"tier_id": tierID,
		"metadata": map[string]string{
			"ide_type":    "ANTIGRAVITY",
			"ide_version": antigravityVersion,
			"ide_name":    "antigravity",
		},
	})

	const maxAttempts = 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		logger.DebugCF("provider.antigravity", "onboardUser polling", map[string]any{
			"attempt": attempt,
			"max":     maxAttempts,
		})

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			baseURL+"/v1internal:onboardUser",
			bytes.NewReader(reqBody),
		)
		if err != nil {
			return "", fmt.Errorf("creating onboardUser request: %w", err)
		}
		applyAntigravityControlHeaders(req, accessToken, customHeaders, userAgent)

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("onboardUser request failed: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil {
			return "", fmt.Errorf("reading onboardUser response: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return "", &AntigravityHTTPError{Operation: "onboardUser", StatusCode: resp.StatusCode}
		}

		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			return "", fmt.Errorf("decoding onboardUser response: %w", err)
		}
		if done, ok := data["done"].(bool); ok && done {
			if responseData, ok := data["response"].(map[string]any); ok {
				if projectID := extractCloudAICompanionProject(responseData); projectID != "" {
					logger.DebugCF("provider.antigravity", "Onboarding succeeded", map[string]any{
						"project_id": projectID,
					})
					return projectID, nil
				}
			}
			return "", fmt.Errorf("onboardUser completed but no project ID in response")
		}
		if attempt < maxAttempts {
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return "", ctx.Err()
			case <-timer.C:
			}
		}
	}
	return "", fmt.Errorf("onboardUser did not complete after %d attempts", maxAttempts)
}

// FetchAntigravityModels fetches available models from the Cloud Code Assist API.
func FetchAntigravityModels(accessToken, projectID string) ([]AntigravityModelInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return FetchAntigravityModelsContext(ctx, accessToken, projectID)
}

type AntigravityModelInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	IsExhausted bool   `json:"is_exhausted"`
}

// --- Helpers ---

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func (p *AntigravityProvider) parseAntigravityError(statusCode int, body []byte) error {
	if statusCode == http.StatusTooManyRequests {
		var errResp struct {
			Error struct {
				Details []map[string]any `json:"details"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil {
			for _, detail := range errResp.Error.Details {
				metadata, ok := detail["metadata"].(map[string]any)
				if !ok {
					continue
				}
				delay, ok := metadata["quotaResetDelay"].(string)
				if !ok {
					continue
				}
				if _, err := time.ParseDuration(delay); err == nil {
					return fmt.Errorf("antigravity rate limit exceeded (reset in %s)", delay)
				}
			}
		}
		return fmt.Errorf("antigravity rate limit exceeded")
	}
	return &AntigravityHTTPError{Operation: "antigravity API", StatusCode: statusCode}
}
