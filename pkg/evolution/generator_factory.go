package evolution

import "github.com/As-tsaqib/picoclaw/pkg/providers"

func NewDraftGeneratorForWorkspace(workspace string, provider providers.LLMProvider, modelID string) DraftGenerator {
	fallback := NewDefaultDraftGenerator(workspace)
	if provider == nil {
		return fallback
	}
	return NewLLMDraftGenerator(provider, modelID, fallback)
}
