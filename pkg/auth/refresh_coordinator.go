package auth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type credentialRefreshFlight struct {
	key        string
	done       chan struct{}
	cancel     context.CancelFunc
	waiters    int
	finished   bool
	credential *AuthCredential
	err        error
}

var credentialRefreshCoordinator = struct {
	sync.Mutex
	flights map[string]*credentialRefreshFlight
}{flights: make(map[string]*credentialRefreshFlight)}

// EnsureFreshCredentialContext returns the authoritative stored credential and
// coordinates a refresh when it will expire inside refreshBefore. Callers using
// the same credential generation share one refresh; unrelated identities use
// different flight keys and do not block one another.
func EnsureFreshCredentialContext(
	ctx context.Context,
	provider string,
	observed *AuthCredential,
	cfg OAuthProviderConfig,
	client *http.Client,
	refreshBefore time.Duration,
) (*AuthCredential, error) {
	return coordinatedCredentialRefresh(ctx, provider, observed, cfg, client, refreshBefore, false)
}

// RefreshCredentialAfterUnauthorizedContext refreshes the credential observed
// by a request that received HTTP 401. If another caller already advanced the
// stored token generation for the same identity, that newer credential is
// reused instead of issuing a second refresh request.
func RefreshCredentialAfterUnauthorizedContext(
	ctx context.Context,
	provider string,
	observed *AuthCredential,
	cfg OAuthProviderConfig,
	client *http.Client,
) (*AuthCredential, error) {
	if observed == nil {
		return nil, fmt.Errorf("credential is required for unauthorized recovery")
	}
	return coordinatedCredentialRefresh(ctx, provider, observed, cfg, client, 0, true)
}

func coordinatedCredentialRefresh(
	ctx context.Context,
	provider string,
	observed *AuthCredential,
	cfg OAuthProviderConfig,
	client *http.Client,
	refreshBefore time.Duration,
	force bool,
) (*AuthCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	current, err := GetCredential(provider)
	if err != nil {
		return nil, fmt.Errorf("loading current credentials: %w", err)
	}
	if current == nil {
		return nil, fmt.Errorf("no credentials for %s", canonicalProvider(provider))
	}

	generationChanged := tokenGenerationChanged(observed, current)
	if generationChanged && credentialIdentityConflicts(observed, current) {
		return nil, fmt.Errorf("credential identity changed during refresh coordination")
	}
	if force && generationChanged {
		return current, nil
	}
	if !force && !credentialNeedsRefreshWithin(current, refreshBefore) {
		return current, nil
	}
	if strings.TrimSpace(current.RefreshToken) == "" {
		if force {
			return nil, fmt.Errorf("no refresh token available")
		}
		return current, nil
	}

	key := credentialRefreshKey(provider, current)
	credentialRefreshCoordinator.Lock()
	if existing := credentialRefreshCoordinator.flights[key]; existing != nil {
		existing.waiters++
		credentialRefreshCoordinator.Unlock()
		return waitForCredentialRefresh(ctx, existing)
	}

	// The network operation belongs to the shared refresh flight rather than to
	// one caller. Individual cancellation removes that waiter; when all waiters
	// are gone the shared context is canceled. A hard timeout bounds the flight.
	flightBase, flightCancel := context.WithCancel(context.Background())
	flight := &credentialRefreshFlight{
		key:     key,
		done:    make(chan struct{}),
		cancel:  flightCancel,
		waiters: 1,
	}
	credentialRefreshCoordinator.flights[key] = flight
	credentialRefreshCoordinator.Unlock()

	go executeCredentialRefreshFlight(
		flightBase,
		key,
		flight,
		provider,
		current,
		cfg,
		client,
		refreshBefore,
		force,
	)
	return waitForCredentialRefresh(ctx, flight)
}

func executeCredentialRefreshFlight(
	flightBase context.Context,
	key string,
	flight *credentialRefreshFlight,
	provider string,
	expected *AuthCredential,
	cfg OAuthProviderConfig,
	client *http.Client,
	refreshBefore time.Duration,
	force bool,
) {
	ctx, cancel := context.WithTimeout(flightBase, defaultOAuthRefreshTimeout)
	defer cancel()

	result, err := refreshCredentialGeneration(ctx, provider, expected, cfg, client, refreshBefore, force)
	flight.cancel()

	credentialRefreshCoordinator.Lock()
	flight.credential = cloneCredential(result)
	flight.err = err
	flight.finished = true
	if credentialRefreshCoordinator.flights[key] == flight {
		delete(credentialRefreshCoordinator.flights, key)
	}
	close(flight.done)
	credentialRefreshCoordinator.Unlock()
}

func refreshCredentialGeneration(
	ctx context.Context,
	provider string,
	expected *AuthCredential,
	cfg OAuthProviderConfig,
	client *http.Client,
	refreshBefore time.Duration,
	force bool,
) (*AuthCredential, error) {
	current, err := GetCredential(provider)
	if err != nil {
		return nil, fmt.Errorf("loading current credentials: %w", err)
	}
	if current == nil {
		return nil, fmt.Errorf("no credentials for %s", canonicalProvider(provider))
	}
	if tokenGenerationChanged(expected, current) {
		if credentialIdentityConflicts(expected, current) {
			return nil, fmt.Errorf("credential identity changed while refresh was in flight")
		}
		return current, nil
	}
	if !force && !credentialNeedsRefreshWithin(current, refreshBefore) {
		return current, nil
	}

	refreshed, err := RefreshAccessTokenWithClientContext(ctx, client, current, cfg)
	if err != nil {
		return nil, err
	}
	persisted, replaced, err := replaceCredentialIfCurrent(provider, current, refreshed)
	if err != nil {
		return nil, fmt.Errorf("saving refreshed token: %w", err)
	}
	if !replaced {
		if persisted != nil {
			if credentialIdentityConflicts(current, persisted) {
				return nil, fmt.Errorf("credential identity changed while refresh was in flight")
			}
			return persisted, nil
		}
		return nil, fmt.Errorf("credential changed while refresh was in flight")
	}
	return persisted, nil
}

func waitForCredentialRefresh(ctx context.Context, flight *credentialRefreshFlight) (*AuthCredential, error) {
	select {
	case <-flight.done:
		return cloneCredential(flight.credential), flight.err
	case <-ctx.Done():
		credentialRefreshCoordinator.Lock()
		if !flight.finished {
			flight.waiters--
			if flight.waiters <= 0 {
				if credentialRefreshCoordinator.flights[flight.key] == flight {
					delete(credentialRefreshCoordinator.flights, flight.key)
				}
				flight.cancel()
			}
		}
		credentialRefreshCoordinator.Unlock()
		return nil, ctx.Err()
	}
}

func credentialNeedsRefreshWithin(cred *AuthCredential, refreshBefore time.Duration) bool {
	if cred == nil || cred.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(refreshBefore).After(cred.ExpiresAt)
}

func tokenGenerationChanged(expected, current *AuthCredential) bool {
	if expected == nil || current == nil {
		return expected != current
	}
	return expected.AccessToken != current.AccessToken || expected.RefreshToken != current.RefreshToken
}

func credentialIdentityConflicts(expected, current *AuthCredential) bool {
	if expected == nil || current == nil {
		return expected != current
	}

	expectedProvider := canonicalProvider(expected.Provider)
	currentProvider := canonicalProvider(current.Provider)
	if expectedProvider != "" && currentProvider != "" && expectedProvider != currentProvider {
		return true
	}

	expectedAccount := strings.TrimSpace(expected.AccountID)
	currentAccount := strings.TrimSpace(current.AccountID)
	if expectedAccount != "" && currentAccount != "" && expectedAccount != currentAccount {
		return true
	}

	expectedEmail := strings.TrimSpace(expected.Email)
	currentEmail := strings.TrimSpace(current.Email)
	if expectedEmail != "" && currentEmail != "" && !strings.EqualFold(expectedEmail, currentEmail) {
		return true
	}

	expectedProject := strings.TrimSpace(expected.ProjectID)
	currentProject := strings.TrimSpace(current.ProjectID)
	if expectedProject != "" && currentProject != "" && expectedProject != currentProject {
		return true
	}
	return false
}

func credentialRefreshKey(provider string, cred *AuthCredential) string {
	if cred == nil {
		return canonicalProvider(provider) + ":none"
	}
	material := strings.Join([]string{cred.AccessToken, cred.RefreshToken}, "\x00")
	sum := sha256.Sum256([]byte(material))
	return canonicalProvider(provider) + ":" + fmt.Sprintf("%x", sum[:])
}
