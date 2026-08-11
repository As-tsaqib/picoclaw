package memory

import "strings"

const RetrievalEngineHybridLexical = "hybrid_lexical"

// RetrievalEngine keeps memory retrieval pluggable without making embeddings
// or an external vector database a runtime dependency. Additional semantic
// engines can implement this interface and preserve the lexical engine as a
// dependency-free fallback.
type RetrievalEngine interface {
	Name() string
	Retrieve(
		store *CuratedStore,
		target string,
		caller CallerScope,
		opts CuratedRetrievalOptions,
	) (CuratedRetrievalResult, error)
}

type HybridLexicalRetrievalEngine struct{}

func (HybridLexicalRetrievalEngine) Name() string { return RetrievalEngineHybridLexical }

func (HybridLexicalRetrievalEngine) Retrieve(
	store *CuratedStore,
	target string,
	caller CallerScope,
	opts CuratedRetrievalOptions,
) (CuratedRetrievalResult, error) {
	return store.Retrieve(target, caller, opts)
}

// NewRetrievalEngine resolves a configured engine. Unknown/empty names fail
// closed to the lightweight local lexical engine for backward compatibility.
func NewRetrievalEngine(name string) RetrievalEngine {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", RetrievalEngineHybridLexical:
		return HybridLexicalRetrievalEngine{}
	default:
		return HybridLexicalRetrievalEngine{}
	}
}
