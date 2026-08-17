package oauthprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/As-tsaqib/picoclaw/pkg/auth"
)

// fetchProjectIDWithRecovery resolves the Antigravity project identity
// and performs at most one generation-aware OAuth refresh/retry after
// an unexpected HTTP 401. Other statuses never trigger a refresh.
func (p *AntigravityProvider) fetchProjectIDWithRecovery(
	ctx context.Context,
	cred *auth.AuthCredential,
) (string, *auth.AuthCredential, error) {
	projectID, err := FetchAntigravityProjectIDWithClientContext(
		ctx,
		p.authHTTPClient(),
		p.apiBaseURL(),
		cred.AccessToken,
		p.customHeaders,
		p.userAgent,
	)
	if err == nil || !isAntigravityUnauthorized(err) {
		return projectID, cred, err
	}

	recovered, refreshErr := auth.RefreshCredentialAfterUnauthorizedContext(
		ctx,
		"google-antigravity",
		cred,
		antigravityOAuthConfig(),
		p.authHTTPClient(),
	)
	if refreshErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", cred, ctxErr
		}
		return "", cred, fmt.Errorf("antigravity project auth recovery: %w", refreshErr)
	}

	projectID, err = FetchAntigravityProjectIDWithClientContext(
		ctx,
		p.authHTTPClient(),
		p.apiBaseURL(),
		recovered.AccessToken,
		p.customHeaders,
		p.userAgent,
	)
	return projectID, recovered, err
}

func isAntigravityUnauthorized(err error) bool {
	var httpErr *AntigravityHTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnauthorized
}
