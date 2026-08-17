package oauthprovider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type antigravityCatalogRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn antigravityCatalogRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestFetchAntigravityModelsContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FetchAntigravityModelsContext(ctx, "test-token", "test-project")
	if err == nil {
		t.Fatal("expected canceled discovery request to fail")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestFetchAntigravityModelsWithClientContextAppliesHeadersSafely(t *testing.T) {
	client := &http.Client{Transport: antigravityCatalogRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer authoritative-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := req.Header.Get("User-Agent"); got != "configured-agent" {
			t.Fatalf("User-Agent = %q", got)
		}
		if got := req.Header.Get("X-Custom"); got != "custom-value" {
			t.Fatalf("X-Custom = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(bytes.NewBufferString(
				`{"models":{"gemini-test":{"displayName":"Gemini Test"}}}`,
			)),
			Request: req,
		}, nil
	})}

	models, err := FetchAntigravityModelsWithClientContext(
		context.Background(),
		client,
		"authoritative-token",
		"project-a",
		map[string]string{
			"Authorization": "Bearer must-not-win",
			"X-Custom":      "custom-value",
		},
		"configured-agent",
	)
	if err != nil {
		t.Fatalf("FetchAntigravityModelsWithClientContext: %v", err)
	}
	if len(models) < 1 || models[0].ID != "gemini-test" {
		t.Fatalf("unexpected models: %#v", models)
	}
}

func TestFetchAntigravityModelsAtBaseURLReturnsOnlyDynamicCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:fetchAvailableModels" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"models":{"account-model":{"displayName":"Account Model"}}}`)
	}))
	defer server.Close()

	models, err := FetchAntigravityModelsAtBaseURLWithClientContext(
		context.Background(), server.Client(), server.URL, "token", "project", nil, "",
	)
	if err != nil {
		t.Fatalf("FetchAntigravityModelsAtBaseURLWithClientContext: %v", err)
	}
	if len(models) != 1 || models[0].ID != "account-model" {
		t.Fatalf("models = %#v, want only dynamic account model", models)
	}
}

func TestFetchAntigravityModelsAtBaseURLEmptyCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"models":{}}`)
	}))
	defer server.Close()

	models, err := FetchAntigravityModelsAtBaseURLWithClientContext(
		context.Background(), server.Client(), server.URL, "token", "project", nil, "",
	)
	if err != nil {
		t.Fatalf("FetchAntigravityModelsAtBaseURLWithClientContext: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("models = %#v, want empty catalog", models)
	}
}

func TestFetchAntigravityModelsAtBaseURLMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{not-json`)
	}))
	defer server.Close()

	_, err := FetchAntigravityModelsAtBaseURLWithClientContext(
		context.Background(), server.Client(), server.URL, "token", "project", nil, "",
	)
	if err == nil || !strings.Contains(err.Error(), "parsing models response") {
		t.Fatalf("error = %v, want parse failure", err)
	}
}

func TestFetchAntigravityModelsAtBaseURLUnauthorizedIsTypedAndSanitized(t *testing.T) {
	const secret = "refresh-token-must-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"`+secret+`"}`)
	}))
	defer server.Close()

	_, err := FetchAntigravityModelsAtBaseURLWithClientContext(
		context.Background(), server.Client(), server.URL, "access-token", "project", nil, "",
	)
	if !IsAntigravityUnauthorized(err) {
		t.Fatalf("error = %v, want typed 401", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked upstream body: %v", err)
	}
}

func TestFetchAntigravityModelsAtBaseURLHonorsTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := FetchAntigravityModelsAtBaseURLWithClientContext(
		ctx, server.Client(), server.URL, "token", "project", nil, "",
	)
	if err == nil || (!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled)) {
		t.Fatalf("error = %v, want cancellation/timeout", err)
	}
}
