package bus

// LocationPayload represents geographical point or venue data.
type LocationPayload struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Title     string  `json:"title,omitempty"`   // If non-empty, sent as venue
	Address   string  `json:"address,omitempty"` // For venue
}

// ContactPayload represents a contact card.
type ContactPayload struct {
	PhoneNumber string `json:"phone_number"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name,omitempty"`
	VCard       string `json:"vcard,omitempty"`
}

// DicePayload represents an animated dice throw.
type DicePayload struct {
	Emoji string `json:"emoji,omitempty"` // 🎲, 🎯, 🏀, ⚽, 🎳, 🎰
}
