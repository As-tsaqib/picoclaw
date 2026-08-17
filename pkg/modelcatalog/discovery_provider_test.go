package modelcatalog

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/auth"
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
	releaseHandler := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-releaseHandler:
		}
	}))
	t.Cleanup(func() {
		close(releaseHandler)
		server.Close()
	})

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

func installCatalogAntigravityCredential(t *testing.T, cred *auth.AuthCredential) {
	t.Helper()
	t.Setenv(config.EnvHome, t.TempDir())
	if err := auth.SetCredential("google-antigravity", cred); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
}

func overrideCatalogAntigravityOAuth(t *testing.T, tokenURL string) {
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

func TestFetchAntigravityDynamicCatalog(t *testing.T) {
	installCatalogAntigravityCredential(t, &auth.AuthCredential{
		AccessToken: "access-valid", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth", ProjectID: "project-a",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer access-valid", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"models":{"z-model":{"displayName":"Z"},"a-model":{"displayName":"A"}}}`)
	}))
	defer server.Close()

	models, err := Fetch(context.Background(), &config.ModelConfig{
		ModelName: "ag", Provider: "antigravity", Model: "antigravity/seed", APIBase: server.URL, Enabled: true,
	})
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "a-model", models[0].ID)
	assert.Equal(t, "z-model", models[1].ID)
}

func TestFetchAntigravity401RefreshRetrySuccess(t *testing.T) {
	var refreshCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls.Add(1)
		_, _ = io.WriteString(w, `{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`)
	}))
	defer tokenServer.Close()
	overrideCatalogAntigravityOAuth(t, tokenServer.URL)
	installCatalogAntigravityCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth", ProjectID: "project-a",
	})
	var apiCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		if r.Header.Get("Authorization") == "Bearer access-old" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		assert.Equal(t, "Bearer access-new", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"models":{"recovered-model":{}}}`)
	}))
	defer server.Close()

	models, err := Fetch(context.Background(), &config.ModelConfig{
		ModelName: "ag", Provider: "antigravity", Model: "antigravity/seed", APIBase: server.URL, Enabled: true,
	})
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "recovered-model", models[0].ID)
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Equal(t, int32(2), apiCalls.Load())
}

func TestFetchAntigravitySecond401Stops(t *testing.T) {
	var refreshCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls.Add(1)
		_, _ = io.WriteString(w, `{"access_token":"access-new","expires_in":3600}`)
	}))
	defer tokenServer.Close()
	overrideCatalogAntigravityOAuth(t, tokenServer.URL)
	installCatalogAntigravityCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth", ProjectID: "project-a",
	})
	var apiCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		apiCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), &config.ModelConfig{
		ModelName: "ag", Provider: "antigravity", Model: "antigravity/seed", APIBase: server.URL, Enabled: true,
	})
	require.Error(t, err)
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Equal(t, int32(2), apiCalls.Load())
}

func TestFetchAntigravityRefreshFailureIsSanitized(t *testing.T) {
	const secret = "refresh-secret-not-in-error"
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, secret)
	}))
	defer tokenServer.Close()
	overrideCatalogAntigravityOAuth(t, tokenServer.URL)
	installCatalogAntigravityCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: secret, ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth", ProjectID: "project-a",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), &config.ModelConfig{
		ModelName: "ag", Provider: "antigravity", Model: "antigravity/seed", APIBase: server.URL, Enabled: true,
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secret)
}

func TestFetchAntigravityConcurrent401SharesRefresh(t *testing.T) {
	const callers = 6
	var refreshCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls.Add(1)
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(w, `{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`)
	}))
	defer tokenServer.Close()
	overrideCatalogAntigravityOAuth(t, tokenServer.URL)
	installCatalogAntigravityCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth", ProjectID: "project-a",
	})

	var oldArrivals atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer access-old" {
			if oldArrivals.Add(1) == callers {
				once.Do(func() { close(release) })
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"models":{"model-new":{}}}`)
	}))
	defer server.Close()
	mc := &config.ModelConfig{
		ModelName: "ag", Provider: "antigravity", Model: "antigravity/seed", APIBase: server.URL, Enabled: true,
	}

	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := Fetch(context.Background(), mc)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(callers), oldArrivals.Load())
	assert.Equal(t, int32(1), refreshCalls.Load())
}

func TestFetchAntigravityCancellationDuringRefresh(t *testing.T) {
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
	overrideCatalogAntigravityOAuth(t, tokenServer.URL)
	installCatalogAntigravityCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth", ProjectID: "project-a",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	mc := &config.ModelConfig{
		ModelName: "ag", Provider: "antigravity", Model: "antigravity/seed", APIBase: server.URL, Enabled: true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Fetch(ctx, mc)
		done <- err
	}()
	<-refreshStarted
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Fetch did not stop after cancellation")
	}
}
