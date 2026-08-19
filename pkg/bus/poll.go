package bus

import (
	"time"
)

// PollPayload represents a channel-neutral native poll or quiz.
type PollPayload struct {
	ID                     string    `json:"id,omitempty"` // local opaque handle
	Mode                   string    `json:"mode"`         // "regular" or "quiz"
	Question               string    `json:"question"`
	Options                []string  `json:"options"`
	IsAnonymous            bool      `json:"is_anonymous,omitempty"`
	AllowsMultipleAnswers  bool      `json:"allows_multiple_answers,omitempty"`
	AllowsRevoting         bool      `json:"allows_revoting,omitempty"`
	ShuffleOptions         bool      `json:"shuffle_options,omitempty"`
	AllowAddingOptions     bool      `json:"allow_adding_options,omitempty"`
	HideResultsUntilCloses bool      `json:"hide_results_until_closes,omitempty"`
	MembersOnly            bool      `json:"members_only,omitempty"`
	CountryCodes           []string  `json:"country_codes,omitempty"`
	CorrectOptionIDs       []int     `json:"correct_option_ids,omitempty"`
	Explanation            string    `json:"explanation,omitempty"`
	OpenPeriodSeconds      int       `json:"open_period_seconds,omitempty"`
	CloseAt                time.Time `json:"close_at,omitempty"`
	IsClosed               bool      `json:"is_closed,omitempty"`
	Description            string    `json:"description,omitempty"`
	FallbackText           string    `json:"fallback_text,omitempty"`
}
