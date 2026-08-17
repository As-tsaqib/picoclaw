package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// RefreshAccessTokenContext refreshes an OAuth credential while honoring ctx.
// The legacy RefreshAccessToken API remains unchanged for existing callers.
func RefreshAccessTokenContext(
	ctx context.Context,
	cred *AuthCredential,
	cfg OAuthProviderConfig,
) (*AuthCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cred == nil {
		return nil, fmt.Errorf("credential is required")
	}
	if cred.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	isGoogle := strings.Contains(strings.ToLower(cfg.Issuer), "accounts.google.com") ||
		(cfg.TokenURL != "" && strings.Contains(cfg.TokenURL, "googleapis.com"))
	data := url.Values{
		"client_id":     {cfg.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {cred.RefreshToken},
	}
	if !isGoogle {
		data.Set("scope", "openid profile email")
	}
	if cfg.ClientSecret != "" {
		data.Set("client_secret", cfg.ClientSecret)
	}

	tokenURL := cfg.Issuer + "/oauth/token"
	if cfg.TokenURL != "" {
		tokenURL = cfg.TokenURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading token refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed with status %d", resp.StatusCode)
	}

	refreshed, err := parseTokenResponse(body, cred.Provider)
	if err != nil {
		return nil, err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = cred.RefreshToken
	}
	if refreshed.AccountID == "" {
		refreshed.AccountID = cred.AccountID
	}
	if cred.Email != "" && refreshed.Email == "" {
		refreshed.Email = cred.Email
	}
	if cred.ProjectID != "" && refreshed.ProjectID == "" {
		refreshed.ProjectID = cred.ProjectID
	}
	return refreshed, nil
}
