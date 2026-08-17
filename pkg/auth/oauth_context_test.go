package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRefreshAccessTokenContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := RefreshAccessTokenContext(ctx, &AuthCredential{
		Provider:     "test",
		RefreshToken: "refresh-secret",
	}, OAuthProviderConfig{
		ClientID: "client",
		TokenURL: server.URL,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled), err)
	require.Less(t, time.Since(started), 2*time.Second)
}
