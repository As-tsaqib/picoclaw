package oauthprovider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/auth"
)

func TestAntigravityProjectDiscovery401RefreshesAndRetriesOnce(t *testing.T) {
	var refreshCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("refresh_token"); got != "refresh-old" {
			t.Fatalf("refresh_token = %q", got)
		}
		_, _ = io.WriteString(w, `{"access_token":"access-new","refresh_token":"refresh-rotated","expires_in":3600}`)
	}))
	defer tokenServer.Close()
	overrideAntigravityOAuthForTest(t, tokenServer.URL)
	installAntigravityTestCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth",
	})

	var projectCalls atomic.Int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projectCalls.Add(1)
		switch r.Header.Get("Authorization") {
		case "Bearer access-old":
			w.WriteHeader(http.StatusUnauthorized)
		case "Bearer access-new":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"cloudaicompanionProject":"project-new"}`)
		default:
			t.Fatalf("unexpected Authorization: %q", r.Header.Get("Authorization"))
		}
	}))
	defer apiServer.Close()

	provider, err := NewAntigravityProviderWithConfig(apiServer.URL, "", "", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := provider.credential(context.Background())
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	if cred.ProjectID != "project-new" || cred.AccessToken != "access-new" || cred.RefreshToken != "refresh-rotated" {
		t.Fatalf("credential = %#v", cred)
	}
	if refreshCalls.Load() != 1 || projectCalls.Load() != 2 {
		t.Fatalf("refresh=%d project=%d, want refresh=1 project=2", refreshCalls.Load(), projectCalls.Load())
	}
	stored, err := auth.GetCredential("google-antigravity")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProjectID != "project-new" || stored.RefreshToken != "refresh-rotated" {
		t.Fatalf("stored credential = %#v", stored)
	}
}

func TestAntigravityProjectDiscoverySecond401Stops(t *testing.T) {
	var refreshCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls.Add(1)
		_, _ = io.WriteString(w, `{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`)
	}))
	defer tokenServer.Close()
	overrideAntigravityOAuthForTest(t, tokenServer.URL)
	installAntigravityTestCredential(t, &auth.AuthCredential{
		AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresAt: time.Now().Add(time.Hour),
		Provider: "google-antigravity", AuthMethod: "oauth",
	})

	var projectCalls atomic.Int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		projectCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer apiServer.Close()
	provider, err := NewAntigravityProviderWithConfig(apiServer.URL, "", "", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.credential(context.Background())
	if err == nil {
		t.Fatal("expected second 401 to stop project discovery")
	}
	if refreshCalls.Load() != 1 || projectCalls.Load() != 2 {
		t.Fatalf("refresh=%d project=%d, want refresh=1 project=2", refreshCalls.Load(), projectCalls.Load())
	}
}
