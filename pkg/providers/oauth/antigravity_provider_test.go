package oauthprovider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/auth"
	"github.com/As-tsaqib/picoclaw/pkg/config"
)

func TestBuildRequestUsesFunctionFieldsWhenToolCallNameMissing(t *testing.T) {
	p := &AntigravityProvider{}

	messages := []Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID: "call_read_file_123",
				Function: &FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"README.md"}`,
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call_read_file_123",
			Content:    "ok",
		},
	}

	req := p.buildRequest(messages, nil, "", nil)
	if len(req.Contents) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(req.Contents))
	}

	modelPart := req.Contents[0].Parts[0]
	if modelPart.FunctionCall == nil {
		t.Fatal("expected functionCall in assistant message")
	}
	if modelPart.FunctionCall.Name != "read_file" {
		t.Fatalf("expected functionCall name read_file, got %q", modelPart.FunctionCall.Name)
	}
	if got := modelPart.FunctionCall.Args["path"]; got != "README.md" {
		t.Fatalf("expected functionCall args[path] to be README.md, got %v", got)
	}

	toolPart := req.Contents[1].Parts[0]
	if toolPart.FunctionResponse == nil {
		t.Fatal("expected functionResponse in tool message")
	}
	if toolPart.FunctionResponse.Name != "read_file" {
		t.Fatalf("expected functionResponse name read_file, got %q", toolPart.FunctionResponse.Name)
	}
}

func TestParseSSEResponse_SplitsThoughtAndVisibleContent(t *testing.T) {
	p := &AntigravityProvider{}
	body := "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hidden reasoning\",\"thought\":true},{\"text\":\"visible answer\"}],\"role\":\"model\"},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":8,\"candidatesTokenCount\":17,\"totalTokenCount\":216}}}\n" +
		"data: [DONE]\n"

	resp, err := p.parseSSEResponse(body)
	if err != nil {
		t.Fatalf("parseSSEResponse() error = %v", err)
	}

	if resp.Content != "visible answer" {
		t.Fatalf("Content = %q, want %q", resp.Content, "visible answer")
	}
	if resp.ReasoningContent != "hidden reasoning" {
		t.Fatalf("ReasoningContent = %q, want %q", resp.ReasoningContent, "hidden reasoning")
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 216 {
		t.Fatalf("Usage.TotalTokens = %v, want %d", resp.Usage, 216)
	}
}

func TestBuildRequest_PreservesComplexToolSchemasByDefault(t *testing.T) {
	p := &AntigravityProvider{}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"parent": map[string]any{
				"anyOf": []any{
					map[string]any{"$ref": "#/$defs/pageParent"},
					map[string]any{"$ref": "#/$defs/databaseParent"},
				},
			},
			"icon": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "null"},
					map[string]any{"$ref": "#/$defs/emoji"},
				},
			},
		},
		"$defs": map[string]any{
			"pageParent": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"page_id": map[string]any{"type": "string"},
				},
				"required": []any{"page_id"},
			},
			"databaseParent": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"database_id": map[string]any{"type": "string"},
				},
				"required": []any{"database_id"},
			},
			"emoji": map[string]any{
				"type":    "string",
				"pattern": "^:[a-z_]+:$",
			},
		},
	}

	req := p.buildRequest(
		[]Message{{Role: "user", Content: "hello"}},
		[]ToolDefinition{{
			Type: "function",
			Function: ToolFunctionDefinition{
				Name:        "mcp_notion_create",
				Description: "Create a Notion object",
				Parameters:  schema,
			},
		}},
		"gemini-3-flash",
		nil,
	)

	if len(req.Tools) != 1 || len(req.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("request tools = %#v, want one function declaration", req.Tools)
	}

	got, ok := req.Tools[0].FunctionDeclarations[0].Parameters.(map[string]any)
	if !ok {
		t.Fatalf("parameters = %#v, want map", req.Tools[0].FunctionDeclarations[0].Parameters)
	}
	if got["$defs"] == nil {
		t.Fatalf("parameters = %#v, want raw schema with $defs preserved by default", got)
	}
}

func antigravityTestSSE(content string) string {
	return `data: {"response":{"candidates":[{"content":{"parts":[{"text":"` + content + `"}],"role":"model"},"finishReason":"STOP"}]}}` + "\n" +
		"data: [DONE]\n"
}

func installAntigravityTestCredential(t *testing.T, cred *auth.AuthCredential) {
	t.Helper()
	t.Setenv(config.EnvHome, t.TempDir())
	if err := auth.SetCredential("google-antigravity", cred); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
}

func overrideAntigravityOAuthForTest(t *testing.T, tokenURL string) {
	t.Helper()
	original := antigravityOAuthConfig
	antigravityOAuthConfig = func() auth.OAuthProviderConfig {
		return auth.OAuthProviderConfig{
			Issuer:       "https://accounts.google.com/o/oauth2/v2",
			TokenURL:     tokenURL,
			ClientID:     "test-client",
			ClientSecret: "test-secret",
		}
	}
	t.Cleanup(func() { antigravityOAuthConfig = original })
}

func TestAntigravityChatUnexpected401RefreshesAndRetriesOnce(t *testing.T) {
	var refreshCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("refresh_token"); got != "refresh-old" {
			t.Fatalf("refresh_token = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"access-new","refresh_token":"refresh-rotated","expires_in":3600}`)
	}))
	defer tokenServer.Close()
	overrideAntigravityOAuthForTest(t, tokenServer.URL)
	installAntigravityTestCredential(t, &auth.AuthCredential{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Add(time.Hour),
		Provider:     "google-antigravity",
		AuthMethod:   "oauth",
		ProjectID:    "project-a",
	})

	var apiCalls atomic.Int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		switch r.Header.Get("Authorization") {
		case "Bearer access-old":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"access-old must not leak"}`)
		case "Bearer access-new":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, antigravityTestSSE("ok"))
		default:
			t.Fatalf("unexpected Authorization: %q", r.Header.Get("Authorization"))
		}
	}))
	defer apiServer.Close()

	provider, err := NewAntigravityProviderWithConfig(apiServer.URL, "", "test-agent", 5, map[string]string{
		"Authorization": "Bearer custom-must-not-win",
	})
	if err != nil {
		t.Fatalf("NewAntigravityProviderWithConfig: %v", err)
	}
	resp, err := provider.Chat(
		context.Background(),
		[]Message{{Role: "user", Content: "hello"}},
		nil,
		"gemini-test",
		nil,
	)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q", resp.Content)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
	if got := apiCalls.Load(); got != 2 {
		t.Fatalf("api calls = %d, want 2", got)
	}
	stored, err := auth.GetCredential("google-antigravity")
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if stored.AccessToken != "access-new" || stored.RefreshToken != "refresh-rotated" {
		t.Fatalf("stored credential = %#v, rotated token was not persisted", stored)
	}
}

func TestAntigravityChatSecond401Stops(t *testing.T) {
	var refreshCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls.Add(1)
		_, _ = io.WriteString(w, `{"access_token":"access-new","expires_in":3600}`)
	}))
	defer tokenServer.Close()
	overrideAntigravityOAuthForTest(t, tokenServer.URL)
	installAntigravityTestCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth", ProjectID: "project-a",
	})

	var apiCalls atomic.Int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		apiCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"still unauthorized"}`)
	}))
	defer apiServer.Close()
	provider, err := NewAntigravityProviderWithConfig(apiServer.URL, "", "", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-test", nil)
	if err == nil {
		t.Fatal("expected second 401 to fail")
	}
	if refreshCalls.Load() != 1 || apiCalls.Load() != 2 {
		t.Fatalf("refresh=%d api=%d, want refresh=1 api=2", refreshCalls.Load(), apiCalls.Load())
	}
}

func TestAntigravityChatRefreshFailureStops(t *testing.T) {
	var refreshCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":"refresh-token-secret"}`)
	}))
	defer tokenServer.Close()
	overrideAntigravityOAuthForTest(t, tokenServer.URL)
	installAntigravityTestCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-token-secret", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth", ProjectID: "project-a",
	})

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer apiServer.Close()
	provider, err := NewAntigravityProviderWithConfig(apiServer.URL, "", "", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-test", nil)
	if err == nil {
		t.Fatal("expected refresh failure")
	}
	if strings.Contains(err.Error(), "refresh-token-secret") {
		t.Fatalf("refresh error leaked credential: %v", err)
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls.Load())
	}
}

func TestAntigravityChatConcurrent401SharesRefresh(t *testing.T) {
	const callers = 8
	var refreshCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls.Add(1)
		time.Sleep(25 * time.Millisecond)
		_, _ = io.WriteString(w, `{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`)
	}))
	defer tokenServer.Close()
	overrideAntigravityOAuthForTest(t, tokenServer.URL)
	installAntigravityTestCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth", ProjectID: "project-a",
	})

	var oldArrivals atomic.Int32
	release401 := make(chan struct{})
	var releaseOnce sync.Once
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer access-old" {
			if oldArrivals.Add(1) == callers {
				releaseOnce.Do(func() { close(release401) })
			}
			select {
			case <-release401:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer access-new" {
			t.Fatalf("unexpected Authorization: %q", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, antigravityTestSSE("ok"))
	}))
	defer apiServer.Close()
	provider, err := NewAntigravityProviderWithConfig(apiServer.URL, "", "", 5, nil)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-test", nil)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Chat: %v", err)
		}
	}
	if got := oldArrivals.Load(); got != callers {
		t.Fatalf("old-token arrivals = %d, want %d", got, callers)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func TestAntigravityChatCancellationStopsRefresh(t *testing.T) {
	refreshStarted := make(chan struct{})
	tokenServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-refreshStarted:
		default:
			close(refreshStarted)
		}
		<-r.Context().Done()
	}))
	defer tokenServer.Close()
	overrideAntigravityOAuthForTest(t, tokenServer.URL)
	installAntigravityTestCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth", ProjectID: "project-a",
	})
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer apiServer.Close()
	provider, err := NewAntigravityProviderWithConfig(apiServer.URL, "", "", 5, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := provider.Chat(ctx, []Message{{Role: "user", Content: "hello"}}, nil, "gemini-test", nil)
		done <- err
	}()
	<-refreshStarted
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Chat did not stop after cancellation")
	}
}

func TestAntigravityChatDoesNotRefreshNon401OrLeakBody(t *testing.T) {
	const secret = "sensitive-upstream-body"
	var refreshCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls.Add(1)
		_, _ = io.WriteString(w, `{"access_token":"should-not-be-used"}`)
	}))
	defer tokenServer.Close()
	overrideAntigravityOAuthForTest(t, tokenServer.URL)
	installAntigravityTestCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth", ProjectID: "project-a",
	})
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, secret)
	}))
	defer apiServer.Close()
	provider, err := NewAntigravityProviderWithConfig(apiServer.URL, "", "", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-test", nil)
	if err == nil {
		t.Fatal("expected forbidden error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked response body: %v", err)
	}
	if got := refreshCalls.Load(); got != 0 {
		t.Fatalf("refresh calls = %d, want 0", got)
	}
}
