package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/fileutil"
)

type AuthCredential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Provider     string    `json:"provider"`
	AuthMethod   string    `json:"auth_method"`
	Email        string    `json:"email,omitempty"`
	ProjectID    string    `json:"project_id,omitempty"`
}

type AuthStore struct {
	Credentials map[string]*AuthCredential `json:"credentials"`
}

var authStoreMu sync.RWMutex

const (
	providerGoogleAntigravity = "google-antigravity"
	providerAntigravityAlias  = "antigravity"
)

func (c *AuthCredential) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(c.ExpiresAt)
}

func (c *AuthCredential) NeedsRefresh() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(5 * time.Minute).After(c.ExpiresAt)
}

func authFilePath() string {
	return filepath.Join(config.GetHome(), "auth.json")
}

func canonicalProvider(provider string) string {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	switch normalized {
	case providerAntigravityAlias:
		return providerGoogleAntigravity
	default:
		return normalized
	}
}

func cloneCredential(cred *AuthCredential) *AuthCredential {
	if cred == nil {
		return nil
	}
	cp := *cred
	return &cp
}

func mergeCredentials(primary, secondary *AuthCredential) *AuthCredential {
	if primary == nil {
		return cloneCredential(secondary)
	}

	merged := *primary
	if secondary == nil {
		return &merged
	}
	if merged.AccessToken == "" {
		merged.AccessToken = secondary.AccessToken
	}
	if merged.RefreshToken == "" {
		merged.RefreshToken = secondary.RefreshToken
	}
	if merged.AccountID == "" {
		merged.AccountID = secondary.AccountID
	}
	if merged.ExpiresAt.IsZero() {
		merged.ExpiresAt = secondary.ExpiresAt
	}
	if merged.Provider == "" {
		merged.Provider = secondary.Provider
	}
	if merged.AuthMethod == "" {
		merged.AuthMethod = secondary.AuthMethod
	}
	if merged.Email == "" {
		merged.Email = secondary.Email
	}
	if merged.ProjectID == "" {
		merged.ProjectID = secondary.ProjectID
	}

	return &merged
}

func shouldPreferCredential(
	candidate *AuthCredential,
	candidateCanonical bool,
	current *AuthCredential,
	currentCanonical bool,
) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}

	switch {
	case candidate.ExpiresAt.After(current.ExpiresAt):
		return true
	case current.ExpiresAt.After(candidate.ExpiresAt):
		return false
	case candidateCanonical != currentCanonical:
		return candidateCanonical
	default:
		return false
	}
}

func normalizeStore(store *AuthStore) {
	if store == nil {
		return
	}
	if store.Credentials == nil {
		store.Credentials = make(map[string]*AuthCredential)
		return
	}

	normalized := make(map[string]*AuthCredential, len(store.Credentials))
	canonicalFlags := make(map[string]bool, len(store.Credentials))

	for provider, cred := range store.Credentials {
		normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
		canonical := canonicalProvider(provider)
		normalizedCred := cloneCredential(cred)
		if normalizedCred != nil {
			normalizedCred.Provider = canonicalProvider(normalizedCred.Provider)
			if normalizedCred.Provider == "" {
				normalizedCred.Provider = canonical
			}
		}

		current := normalized[canonical]
		currentCanonical := canonicalFlags[canonical]
		candidateCanonical := normalizedProvider == canonical

		if shouldPreferCredential(normalizedCred, candidateCanonical, current, currentCanonical) {
			normalized[canonical] = mergeCredentials(normalizedCred, current)
			canonicalFlags[canonical] = candidateCanonical
			continue
		}

		normalized[canonical] = mergeCredentials(current, normalizedCred)
	}

	store.Credentials = normalized
}

func loadStoreUnlocked() (*AuthStore, error) {
	path := authFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &AuthStore{Credentials: make(map[string]*AuthCredential)}, nil
		}
		return nil, err
	}

	var store AuthStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	normalizeStore(&store)
	return &store, nil
}

func saveStoreUnlocked(store *AuthStore) error {
	path := authFilePath()
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	// Use unified atomic write utility with explicit sync for flash storage reliability.
	return fileutil.WriteFileAtomic(path, data, 0o600)
}

func LoadStore() (*AuthStore, error) {
	authStoreMu.RLock()
	defer authStoreMu.RUnlock()
	return loadStoreUnlocked()
}

func SaveStore(store *AuthStore) error {
	authStoreMu.Lock()
	defer authStoreMu.Unlock()
	return saveStoreUnlocked(store)
}

func GetCredential(provider string) (*AuthCredential, error) {
	authStoreMu.RLock()
	defer authStoreMu.RUnlock()

	store, err := loadStoreUnlocked()
	if err != nil {
		return nil, err
	}
	cred, ok := store.Credentials[canonicalProvider(provider)]
	if !ok {
		return nil, nil
	}
	return cloneCredential(cred), nil
}

func normalizedCredential(provider string, cred *AuthCredential) *AuthCredential {
	canonical := canonicalProvider(provider)
	normalized := cloneCredential(cred)
	if normalized != nil {
		normalized.Provider = canonicalProvider(normalized.Provider)
		if normalized.Provider == "" {
			normalized.Provider = canonical
		}
	}
	return normalized
}

func SetCredential(provider string, cred *AuthCredential) error {
	authStoreMu.Lock()
	defer authStoreMu.Unlock()

	store, err := loadStoreUnlocked()
	if err != nil {
		return err
	}
	store.Credentials[canonicalProvider(provider)] = normalizedCredential(provider, cred)
	return saveStoreUnlocked(store)
}

// UpdateCredentialIfCurrent atomically applies metadata changes only if the
// stored access/refresh token generation and credential identity still match
// expected. It performs a three-way merge so an in-flight metadata lookup cannot
// roll back unrelated metadata that another caller updated concurrently.
func UpdateCredentialIfCurrent(
	provider string,
	expected, replacement *AuthCredential,
) (*AuthCredential, bool, error) {
	authStoreMu.Lock()
	defer authStoreMu.Unlock()

	store, err := loadStoreUnlocked()
	if err != nil {
		return nil, false, err
	}
	canonical := canonicalProvider(provider)
	current := store.Credentials[canonical]
	if current == nil {
		return nil, false, nil
	}
	if !credentialTokenGenerationMatches(expected, current) || credentialIdentityConflicts(expected, current) {
		return cloneCredential(current), false, nil
	}

	normalizedExpected := normalizedCredential(provider, expected)
	normalizedReplacement := normalizedCredential(provider, replacement)
	if normalizedExpected == nil || normalizedReplacement == nil {
		return cloneCredential(current), false, nil
	}

	updated := cloneCredential(current)
	mergeCredentialMetadataField(&updated.Provider, normalizedExpected.Provider, normalizedReplacement.Provider)
	mergeCredentialMetadataField(&updated.AuthMethod, normalizedExpected.AuthMethod, normalizedReplacement.AuthMethod)
	mergeCredentialMetadataField(&updated.AccountID, normalizedExpected.AccountID, normalizedReplacement.AccountID)
	mergeCredentialMetadataField(&updated.Email, normalizedExpected.Email, normalizedReplacement.Email)
	mergeCredentialMetadataField(&updated.ProjectID, normalizedExpected.ProjectID, normalizedReplacement.ProjectID)

	store.Credentials[canonical] = updated
	if err := saveStoreUnlocked(store); err != nil {
		return nil, false, err
	}
	return cloneCredential(updated), true, nil
}

func mergeCredentialMetadataField(current *string, expected, replacement string) {
	if current == nil || replacement == expected || *current != expected {
		return
	}
	*current = replacement
}

// replaceCredentialIfCurrent atomically applies a refreshed token generation
// only when the stored tokens still match expected. Metadata is preserved from
// the authoritative current store so an in-flight refresh cannot roll back a
// concurrent account/project metadata update.
func replaceCredentialIfCurrent(
	provider string,
	expected, replacement *AuthCredential,
) (*AuthCredential, bool, error) {
	authStoreMu.Lock()
	defer authStoreMu.Unlock()

	store, err := loadStoreUnlocked()
	if err != nil {
		return nil, false, err
	}
	canonical := canonicalProvider(provider)
	current := store.Credentials[canonical]
	if current == nil {
		return nil, false, nil
	}
	if !credentialTokenGenerationMatches(expected, current) {
		return cloneCredential(current), false, nil
	}

	normalized := normalizedCredential(provider, replacement)
	if normalized == nil {
		return nil, false, nil
	}
	updated := cloneCredential(current)
	updated.AccessToken = normalized.AccessToken
	updated.RefreshToken = normalized.RefreshToken
	updated.ExpiresAt = normalized.ExpiresAt
	if updated.Provider == "" {
		updated.Provider = normalized.Provider
	}
	if updated.AuthMethod == "" {
		updated.AuthMethod = normalized.AuthMethod
	}
	if updated.AccountID == "" {
		updated.AccountID = normalized.AccountID
	}
	if updated.Email == "" {
		updated.Email = normalized.Email
	}
	if updated.ProjectID == "" {
		updated.ProjectID = normalized.ProjectID
	}
	store.Credentials[canonical] = updated
	if err := saveStoreUnlocked(store); err != nil {
		return nil, false, err
	}
	return cloneCredential(updated), true, nil
}

func credentialTokenGenerationMatches(expected, current *AuthCredential) bool {
	if expected == nil || current == nil {
		return expected == current
	}
	return current.AccessToken == expected.AccessToken && current.RefreshToken == expected.RefreshToken
}

func DeleteCredential(provider string) error {
	authStoreMu.Lock()
	defer authStoreMu.Unlock()

	store, err := loadStoreUnlocked()
	if err != nil {
		return err
	}
	delete(store.Credentials, canonicalProvider(provider))
	return saveStoreUnlocked(store)
}

func DeleteAllCredentials() error {
	authStoreMu.Lock()
	defer authStoreMu.Unlock()

	path := authFilePath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
