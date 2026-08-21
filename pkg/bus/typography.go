package bus

import "strings"

// CardHeaderKind selects contextual two-column vocabulary for structured cards.
type CardHeaderKind string

const (
	CardHeaderDetail    CardHeaderKind = "detail"
	CardHeaderStatus    CardHeaderKind = "status"
	CardHeaderInventory CardHeaderKind = "inventory"

	DetailHeaderPlain    = "ATTRIBUTE | DETAILS"
	StatusHeaderPlain    = "METRIC | STATUS"
	InventoryHeaderPlain = "ITEM | DETAILS"

	DetailHeaderStyled    = "𝔸𝕋𝕋ℝ𝕀𝔹𝕌𝕋𝔼 | 𝔻𝔼𝕋𝔸𝕀𝕃𝕊"
	StatusHeaderStyled    = "𝕄𝔼𝕋ℝ𝕀ℂ | 𝕊𝕋𝔸𝕋𝕌𝕊"
	InventoryHeaderStyled = "𝕀𝕋𝔼𝕄 | 𝔻𝔼𝕋𝔸𝕀𝕃𝕊"
)

// CardHeader returns the exact display header. Styling is presentation-only;
// callers must continue to use plain canonical strings for IDs, lookup keys,
// callbacks, state, logs, URLs, paths, and tokens.
func CardHeader(kind CardHeaderKind, styled bool) string {
	if styled {
		switch kind {
		case CardHeaderStatus:
			return StatusHeaderStyled
		case CardHeaderInventory:
			return InventoryHeaderStyled
		default:
			return DetailHeaderStyled
		}
	}
	switch kind {
	case CardHeaderStatus:
		return StatusHeaderPlain
	case CardHeaderInventory:
		return InventoryHeaderPlain
	default:
		return DetailHeaderPlain
	}
}

// CardHeaderColumns returns the two display columns for a structured table.
func CardHeaderColumns(kind CardHeaderKind, styled bool) []string {
	parts := strings.SplitN(CardHeader(kind, styled), " | ", 2)
	if len(parts) != 2 {
		return []string{"ATTRIBUTE", "DETAILS"}
	}
	return parts
}

// NormalizeCardTypography upgrades legacy generic two-column display headers at
// the presentation boundary. It deliberately changes display vocabulary only:
// row values, IDs, callback state, lookup keys, routes, paths, and tokens are
// never rewritten. New builders should still choose CardHeaderColumns directly;
// this compatibility normalizer keeps older structured producers consistent
// while they pass through the shared command/callback presentation boundary.
func NormalizeCardTypography(content *StructuredContent) {
	if content == nil {
		return
	}
	kind := cardHeaderKindForContent(content.Kind)
	for i := range content.Tables {
		normalizeStructuredTableHeader(&content.Tables[i], kind)
	}
	normalizeStructuredBlockHeaders(content.Blocks, kind, 0)
}

func normalizeStructuredBlockHeaders(blocks []StructuredBlock, kind CardHeaderKind, depth int) {
	if depth >= structuredBlockMaxDepth {
		return
	}
	for i := range blocks {
		if blocks[i].Table != nil {
			normalizeStructuredTableHeader(blocks[i].Table, kind)
		}
		normalizeStructuredBlockHeaders(blocks[i].Blocks, kind, depth+1)
	}
}

func normalizeStructuredTableHeader(table *StructuredTable, kind CardHeaderKind) {
	if table == nil || !legacyGenericCardHeader(table.Columns) {
		return
	}
	table.Columns = CardHeaderColumns(kind, true)
}

func legacyGenericCardHeader(columns []string) bool {
	if len(columns) != 2 {
		return false
	}
	left := strings.ToLower(strings.TrimSpace(columns[0]))
	right := strings.ToLower(strings.TrimSpace(columns[1]))
	if right != "nilai" {
		return false
	}
	return left == "properti" || left == "metrik"
}

func cardHeaderKindForContent(kind string) CardHeaderKind {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch {
	case strings.Contains(kind, "status"), strings.Contains(kind, "health"), strings.Contains(kind, "check"):
		return CardHeaderStatus
	case strings.Contains(kind, "list"), strings.Contains(kind, "inventory"), strings.Contains(kind, "catalog"):
		return CardHeaderInventory
	default:
		return CardHeaderDetail
	}
}
