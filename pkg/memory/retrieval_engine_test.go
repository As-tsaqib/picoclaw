package memory

import "testing"

func TestNewRetrievalEngineKeepsLexicalFallback(t *testing.T) {
	for _, value := range []string{"", "hybrid_lexical", "future_semantic_engine"} {
		engine := NewRetrievalEngine(value)
		if engine == nil || engine.Name() != RetrievalEngineHybridLexical {
			t.Fatalf("engine(%q) = %#v", value, engine)
		}
	}
}
