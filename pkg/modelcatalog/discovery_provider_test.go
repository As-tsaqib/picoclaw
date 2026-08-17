package modelcatalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/config"
)

func TestFetchOllamaCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2:latest"},{"model":"qwen3:latest"}]}`))
	}))
	defer server.Close()

	models, err := Fetch(context.Background(), &config.ModelConfig{
		ModelName: "ollama",
		Provider:  "ollama",
		Model:     "ollama/seed",
		APIBase:   server.URL + "/v1",
		Enabled:   true,
	})
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "llama3.2:latest", models[0].ID)
	assert.Equal(t, "qwen3:latest", models[1].ID)
}

func TestFetchNearAICatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/model/list", r.URL.Path)
		assert.Equal(t, "Bearer near-secret", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"modelId":"near/model-a","metadata":{"ownedBy":"near"}}]}`))
	}))
	defer server.Close()

	mc := &config.ModelConfig{
		ModelName: "near",
		Provider:  "nearai",
		Model:     "nearai/seed",
		APIBase:   server.URL,
		Enabled:   true,
	}
	mc.SetAPIKey("near-secret")
	models, err := Fetch(context.Background(), mc)
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "near/model-a", models[0].ID)
	assert.Equal(t, "near", models[0].OwnedBy)
}

func TestFetchOpenAICompatibleHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	mc := &config.ModelConfig{
		ModelName: "slow",
		Provider:  "openai",
		Model:     "openai/seed",
		APIBase:   server.URL,
		Enabled:   true,
	}
	started := time.Now()
	_, err := Fetch(ctx, mc)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled), err)
	assert.Less(t, time.Since(started), 2*time.Second)
}
