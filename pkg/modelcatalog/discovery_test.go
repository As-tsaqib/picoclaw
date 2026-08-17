package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/config"
)

func TestFetchOpenAICompatibleNormalizesModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization header = %q", got)
		}
		if got := r.Header.Get("X-Discovery-Test"); got != "custom" {
			t.Fatalf("custom header = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "picoclaw-model-test" {
			t.Fatalf("user agent = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"z-model"},{"id":"a-model"},{"id":"a-model"}]}`))
	}))
	defer server.Close()

	mc := &config.ModelConfig{
		ModelName: "test",
		Provider:  "openai",
		Model:     "seed",
		APIBase:   server.URL,
		Enabled:   true,
		CustomHeaders: map[string]string{
			"Authorization":    "Bearer must-not-win",
			"X-Discovery-Test": "custom",
		},
		UserAgent: "picoclaw-model-test",
	}
	mc.SetAPIKey("secret")
	models, err := Fetch(context.Background(), mc)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(models) != 2 || models[0].ID != "a-model" || models[1].ID != "z-model" {
		t.Fatalf("unexpected models: %#v", models)
	}
}

func TestFetchUsesConfiguredProxy(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Host; got != "catalog.invalid" {
			t.Fatalf("proxy target host = %q", got)
		}
		if got := r.URL.Path; got != "/v1/models" {
			t.Fatalf("proxy target path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"proxied-model"}]}`))
	}))
	defer proxy.Close()

	mc := &config.ModelConfig{
		ModelName: "proxied",
		Provider:  "openai",
		Model:     "seed",
		APIBase:   "http://catalog.invalid/v1",
		Proxy:     proxy.URL,
		Enabled:   true,
	}
	models, err := Fetch(context.Background(), mc)
	if err != nil {
		t.Fatalf("Fetch through proxy: %v", err)
	}
	if len(models) != 1 || models[0].ID != "proxied-model" {
		t.Fatalf("unexpected proxied models: %#v", models)
	}
}

func TestFetchRejectsUnsupportedProvider(t *testing.T) {
	mc := &config.ModelConfig{ModelName: "claude", Provider: "anthropic", Model: "claude-test", Enabled: true}
	if _, err := Fetch(context.Background(), mc); err == nil {
		t.Fatal("expected unsupported discovery error")
	}
}
