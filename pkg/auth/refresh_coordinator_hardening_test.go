package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestEnsureFreshCredentialContextPreservesConcurrentMetadataUpdate(t *testing.T) {
	setTestAuthHome(t)
	original := antigravityTestCredential("access-old", "refresh-old")
	original.ProjectID = "project-old"
	if err := SetCredential("google-antigravity", original); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`,
			)),
			Request: r,
		}, nil
	})}

	resultCh := make(chan *AuthCredential, 1)
	errCh := make(chan error, 1)
	go func() {
		cred, err := EnsureFreshCredentialContext(
			context.Background(), "google-antigravity", original,
			oauthTestConfig("https://oauth.example.invalid/token"), client, 5*time.Minute,
		)
		resultCh <- cred
		errCh <- err
	}()
	<-started

	metadataUpdate := *original
	metadataUpdate.ProjectID = "project-new"
	if err := SetCredential("google-antigravity", &metadataUpdate); err != nil {
		t.Fatal(err)
	}
	close(release)

	if err := <-errCh; err != nil {
		t.Fatalf("refresh error = %v", err)
	}
	result := <-resultCh
	if result.AccessToken != "access-new" || result.RefreshToken != "refresh-new" {
		t.Fatalf("result token generation = %#v", result)
	}
	if result.ProjectID != "project-new" {
		t.Fatalf("result ProjectID = %q, want concurrent metadata update", result.ProjectID)
	}
	stored, err := GetCredential("google-antigravity")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProjectID != "project-new" || stored.AccessToken != "access-new" || stored.RefreshToken != "refresh-new" {
		t.Fatalf("stored credential = %#v", stored)
	}
}

func TestRefreshCredentialAfterUnauthorizedRejectsIdentityChange(t *testing.T) {
	setTestAuthHome(t)
	observed := antigravityTestCredential("access-old", "refresh-old")
	observed.AccountID = "account-a"
	observed.Email = "a@example.com"
	observed.ProjectID = "project-a"

	current := antigravityTestCredential("access-new", "refresh-new")
	current.AccountID = "account-b"
	current.Email = "b@example.com"
	current.ProjectID = "project-b"
	if err := SetCredential("google-antigravity", current); err != nil {
		t.Fatal(err)
	}

	_, err := RefreshCredentialAfterUnauthorizedContext(
		context.Background(), "google-antigravity", observed,
		OAuthProviderConfig{TokenURL: "http://127.0.0.1:1"}, nil,
	)
	if err == nil {
		t.Fatal("expected account identity change to reject 401 credential reuse")
	}
	if !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("error = %v, want identity-change rejection", err)
	}
	for _, secret := range []string{observed.AccessToken, observed.RefreshToken, current.AccessToken, current.RefreshToken} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked credential %q: %v", secret, err)
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected context error: %v", err)
	}
}
