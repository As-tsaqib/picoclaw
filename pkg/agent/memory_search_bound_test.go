package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemorySearchInputIsBoundedBeforeStoreAccess(t *testing.T) {
	service := memoryCommandService{}
	_, err := service.search("   ", 10)
	require.EqualError(t, err, "memory search query is empty")
	_, err = service.search(strings.Repeat("x", memorySearchQueryMaxRunes+1), 10)
	require.EqualError(t, err, "memory search query is too long")
}
