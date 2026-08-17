package oauthprovider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
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
