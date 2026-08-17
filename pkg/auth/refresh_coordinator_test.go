package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func antigravityTestCredential(access, refresh string) *AuthCredential {
	return &AuthCredential{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    time.Now().Add(time.Minute),
		Provider:     "google-antigravity",
		AuthMethod:   "oauth",
		Email:        "user@example.com",
		ProjectID:    "project-a",
	}
}

func oauthTestConfig(tokenURL string) OAuthProviderConfig {
	return OAuthProviderConfig{ClientID: "client", TokenURL: tokenURL}
}

func TestEnsureFreshCredentialContextPersistsRotatedRefreshToken(t *testing.T) {
	setTestAuthHome(t)
	original := antigravityTestCredential("access-old", "refresh-old")
	if err := SetCredential("google-antigravity", original); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.FormValue("refresh_token"); got != "refresh-old" {
			t.Fatalf("refresh_token = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-new",
			"refresh_token": "refresh-rotated",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	got, err := EnsureFreshCredentialContext(
		context.Background(), "google-antigravity", original,
		oauthTestConfig(server.URL), nil, 5*time.Minute,
	)
	if err != nil {
		t.Fatalf("EnsureFreshCredentialContext() error = %v", err)
	}
	if got.RefreshToken != "refresh-rotated" || got.AccessToken != "access-new" {
		t.Fatalf("credential = %#v", got)
	}
	stored, err := GetCredential("google-antigravity")
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefreshToken != "refresh-rotated" {
		t.Fatalf("stored refresh token = %q", stored.RefreshToken)
	}
}

func TestEnsureFreshCredentialContextConcurrentSameAccountRefreshesOnce(t *testing.T) {
	setTestAuthHome(t)
	original := antigravityTestCredential("access-old", "refresh-old")
	if err := SetCredential("google-antigravity", original); err != nil {
		t.Fatal(err)
	}

	var refreshes atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if refreshes.Add(1) == 1 {
			close(started)
		}
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-new",
			"refresh_token": "refresh-new",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	const callers = 12
	results := make(chan *AuthCredential, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cred, err := EnsureFreshCredentialContext(
				context.Background(), "google-antigravity", original,
				oauthTestConfig(server.URL), nil, 5*time.Minute,
			)
			results <- cred
			errs <- err
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent refresh error = %v", err)
		}
	}
	for cred := range results {
		if cred == nil || cred.AccessToken != "access-new" || cred.RefreshToken != "refresh-new" {
			t.Fatalf("concurrent credential = %#v", cred)
		}
	}
	if got := refreshes.Load(); got != 1 {
		t.Fatalf("refresh requests = %d, want 1", got)
	}
}

func TestEnsureFreshCredentialContextDifferentAccountsDoNotBlock(t *testing.T) {
	setTestAuthHome(t)
	a := antigravityTestCredential("access-a", "refresh-a")
	a.Provider = "account-a"
	a.Email = "a@example.com"
	b := antigravityTestCredential("access-b", "refresh-b")
	b.Provider = "account-b"
	b.Email = "b@example.com"
	if err := SetCredential("account-a", a); err != nil {
		t.Fatal(err)
	}
	if err := SetCredential("account-b", b); err != nil {
		t.Fatal(err)
	}

	aStarted := make(chan struct{})
	releaseA := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.FormValue("refresh_token") {
		case "refresh-a":
			close(aStarted)
			<-releaseA
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-a", "expires_in": 3600})
		case "refresh-b":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-b", "expires_in": 3600})
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	aDone := make(chan error, 1)
	go func() {
		_, err := EnsureFreshCredentialContext(
			context.Background(), "account-a", a, oauthTestConfig(server.URL), nil, 5*time.Minute,
		)
		aDone <- err
	}()
	<-aStarted

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	gotB, err := EnsureFreshCredentialContext(ctx, "account-b", b, oauthTestConfig(server.URL), nil, 5*time.Minute)
	if err != nil {
		t.Fatalf("account B refresh was blocked: %v", err)
	}
	if gotB.AccessToken != "new-b" {
		t.Fatalf("account B access token = %q", gotB.AccessToken)
	}
	close(releaseA)
	if err := <-aDone; err != nil {
		t.Fatalf("account A refresh error = %v", err)
	}
}

func TestEnsureFreshCredentialContextDoesNotOverwriteNewerGeneration(t *testing.T) {
	setTestAuthHome(t)
	original := antigravityTestCredential("access-old", "refresh-old")
	if err := SetCredential("google-antigravity", original); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-stale-result",
			"refresh_token": "refresh-stale-result",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	resultCh := make(chan *AuthCredential, 1)
	errCh := make(chan error, 1)
	go func() {
		cred, err := EnsureFreshCredentialContext(
			context.Background(), "google-antigravity", original,
			oauthTestConfig(server.URL), nil, 5*time.Minute,
		)
		resultCh <- cred
		errCh <- err
	}()
	<-started

	newer := antigravityTestCredential("access-authoritative", "refresh-authoritative")
	newer.ExpiresAt = time.Now().Add(time.Hour)
	if err := SetCredential("google-antigravity", newer); err != nil {
		t.Fatal(err)
	}
	close(release)

	if err := <-errCh; err != nil {
		t.Fatalf("refresh error = %v", err)
	}
	result := <-resultCh
	if result.AccessToken != "access-authoritative" || result.RefreshToken != "refresh-authoritative" {
		t.Fatalf("result = %#v", result)
	}
	stored, err := GetCredential("google-antigravity")
	if err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "access-authoritative" || stored.RefreshToken != "refresh-authoritative" {
		t.Fatalf("stored = %#v", stored)
	}
}

func TestRefreshCredentialAfterUnauthorizedReusesAdvancedGeneration(t *testing.T) {
	setTestAuthHome(t)
	observed := antigravityTestCredential("access-old", "refresh-old")
	newer := antigravityTestCredential("access-new", "refresh-new")
	newer.ExpiresAt = time.Now().Add(time.Hour)
	if err := SetCredential("google-antigravity", newer); err != nil {
		t.Fatal(err)
	}

	got, err := RefreshCredentialAfterUnauthorizedContext(
		context.Background(), "google-antigravity", observed,
		OAuthProviderConfig{TokenURL: "http://127.0.0.1:1"}, nil,
	)
	if err != nil {
		t.Fatalf("RefreshCredentialAfterUnauthorizedContext() error = %v", err)
	}
	if got.AccessToken != "access-new" {
		t.Fatalf("access token = %q", got.AccessToken)
	}
}

func TestEnsureFreshCredentialContextCancellationStopsRefresh(t *testing.T) {
	setTestAuthHome(t)
	original := antigravityTestCredential("access-old", "refresh-old")
	if err := SetCredential("google-antigravity", original); err != nil {
		t.Fatal(err)
	}

	requestStarted := make(chan struct{})
	requestCanceled := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-r.Context().Done()
		close(requestCanceled)
		return nil, r.Context().Err()
	})}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := EnsureFreshCredentialContext(
			ctx, "google-antigravity", original,
			oauthTestConfig("https://oauth.example.invalid/token"), client, 5*time.Minute,
		)
		done <- err
	}()
	<-requestStarted
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("refresh caller did not stop after cancellation")
	}
	select {
	case <-requestCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh HTTP request context was not canceled")
	}
}

func TestCredentialRefreshKeyDoesNotContainPlaintextToken(t *testing.T) {
	cred := antigravityTestCredential("access-secret-value", "refresh-secret-value")
	key := credentialRefreshKey("google-antigravity", cred)
	for _, secret := range []string{cred.AccessToken, cred.RefreshToken} {
		if strings.Contains(key, secret) {
			t.Fatalf("refresh key contains plaintext secret %q: %q", secret, key)
		}
	}
}

func TestCredentialRefreshKeyIgnoresMetadataWithinTokenGeneration(t *testing.T) {
	cred := antigravityTestCredential("access-secret-value", "refresh-secret-value")
	key := credentialRefreshKey("google-antigravity", cred)
	changed := *cred
	changed.Provider = "antigravity"
	changed.AccountID = "different-account-metadata"
	changed.Email = "different@example.com"
	changed.ProjectID = "different-project"
	if got := credentialRefreshKey("google-antigravity", &changed); got != key {
		t.Fatalf("metadata split one token generation: got %q, want %q", got, key)
	}
}

func TestEnsureFreshCredentialContextCanceledFlightDoesNotPoisonNextCaller(t *testing.T) {
	setTestAuthHome(t)
	original := antigravityTestCredential("access-old", "refresh-old")
	if err := SetCredential("google-antigravity", original); err != nil {
		t.Fatal(err)
	}

	var requests atomic.Int32
	firstStarted := make(chan struct{})
	firstCanceled := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch requests.Add(1) {
		case 1:
			close(firstStarted)
			<-r.Context().Done()
			close(firstCanceled)
			return nil, r.Context().Err()
		case 2:
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`,
				)),
				Request: r,
			}, nil
		default:
			return nil, errors.New("unexpected refresh request")
		}
	})}

	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := EnsureFreshCredentialContext(
			ctx, "google-antigravity", original,
			oauthTestConfig("https://oauth.example.invalid/token"), client, 5*time.Minute,
		)
		firstDone <- err
	}()
	<-firstStarted
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first error = %v, want context.Canceled", err)
	}
	select {
	case <-firstCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("first refresh HTTP request context was not canceled")
	}

	got, err := EnsureFreshCredentialContext(
		context.Background(), "google-antigravity", original,
		oauthTestConfig("https://oauth.example.invalid/token"), client, 5*time.Minute,
	)
	if err != nil {
		t.Fatalf("second refresh error = %v", err)
	}
	if got.AccessToken != "access-new" || got.RefreshToken != "refresh-new" {
		t.Fatalf("second credential = %#v", got)
	}
	if gotRequests := requests.Load(); gotRequests != 2 {
		t.Fatalf("refresh requests = %d, want 2", gotRequests)
	}
}
