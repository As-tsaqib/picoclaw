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
