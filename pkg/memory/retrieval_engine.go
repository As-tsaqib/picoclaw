package memory

import (
	"math"
	"strings"
)

const (
	RetrievalEngineHybridLexical  = "hybrid_lexical"
	RetrievalEngineSemanticRerank = "semantic_rerank"
)

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

// SemanticScorer is intentionally provider-neutral. Implementations may use a
// local model, embeddings, or another bounded scorer without changing store
// ownership or privacy enforcement. Scores must be in the 0..1 range.
type SemanticScorer interface {
	Score(query, candidate string) float64
}

type SemanticRerankRetrievalEngine struct {
	scorer SemanticScorer
}

func (SemanticRerankRetrievalEngine) Name() string { return RetrievalEngineSemanticRerank }

func (e SemanticRerankRetrievalEngine) Retrieve(
	store *CuratedStore,
	target string,
	caller CallerScope,
	opts CuratedRetrievalOptions,
) (CuratedRetrievalResult, error) {
	if e.scorer == nil {
		return HybridLexicalRetrievalEngine{}.Retrieve(store, target, caller, opts)
	}
	opts.SemanticScore = e.scorer.Score
	if opts.SemanticWeight <= 0 {
		opts.SemanticWeight = 2.5
	}
	return store.Retrieve(target, caller, opts)
}

// conceptSemanticScorer is a tiny deterministic multilingual concept mapper.
// It is not an embedding model; it covers durable interaction dimensions and
// common paraphrases while retaining the lexical engine as a general fallback.
// The interface above permits a richer scorer later without a store migration.
type conceptSemanticScorer struct{}

var semanticConceptPhrases = map[string][]string{
	"verbosity.concise": {
		"concise", "brief", "short answer", "not too long", "don't explain it too long",
		"singkat", "ringkas", "jangan panjang", "jangan kepanjangan", "tidak terlalu panjang",
		"jangan jelaskan kepanjangan", "jangan terlalu panjang", "jangan jelaskan terlalu panjang",
		"مختصر", "باختصار", "لا تطيل",
	},
	"verbosity.detailed": {
		"detailed", "in depth", "thorough", "step by step", "detail", "lengkap", "rinci",
		"mendalam", "langkah demi langkah", "مفصل", "بالتفصيل",
	},
	"command.copy_ready": {
		"copy paste", "copy-paste", "ready to run", "directly run", "paste and run",
		"tinggal jalankan", "tinggal saya jalankan", "langsung jalankan", "siap salin", "salin tempel",
		"copy dan paste",
		"جاهز للتنفيذ", "انسخ والصق",
	},
	"examples": {
		"example", "examples", "sample", "show me how", "contoh", "misalnya", "contohnya",
		"مثال", "أمثلة",
	},
	"language.indonesian": {
		"indonesian", "bahasa indonesia", "jawab indonesia", "respond in indonesian", "الإندونيسية",
	},
	"language.english": {
		"english", "bahasa inggris", "respond in english", "الإنجليزية",
	},
	"language.arabic": {
		"arabic", "bahasa arab", "respond in arabic", "العربية",
	},
}

func (conceptSemanticScorer) Score(query, candidate string) float64 {
	left := semanticConcepts(query)
	right := semanticConcepts(candidate)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	intersection := 0
	for concept := range left {
		if _, ok := right[concept]; ok {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	return math.Min(1, (2*float64(intersection))/float64(len(left)+len(right)))
}

func semanticConcepts(value string) map[string]struct{} {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	if value == "" {
		return nil
	}
	out := make(map[string]struct{})
	for concept, phrases := range semanticConceptPhrases {
		for _, phrase := range phrases {
			if strings.Contains(value, phrase) {
				out[concept] = struct{}{}
				break
			}
		}
	}
	return out
}

// NewRetrievalEngine resolves a configured engine. Unknown/empty names fail
// closed to the lightweight local lexical engine for backward compatibility.
func NewRetrievalEngine(name string) RetrievalEngine {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", RetrievalEngineHybridLexical:
		return HybridLexicalRetrievalEngine{}
	case RetrievalEngineSemanticRerank:
		return SemanticRerankRetrievalEngine{scorer: conceptSemanticScorer{}}
	default:
		return HybridLexicalRetrievalEngine{}
	}
}
