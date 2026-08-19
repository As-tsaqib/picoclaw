package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	toolshared "github.com/As-tsaqib/picoclaw/pkg/tools/shared"
)

// SendLocationTool sends geographic location or venue data.
type sendLocationTool struct{}

func NewSendLocationTool() Tool {
	return &sendLocationTool{}
}

func (t *sendLocationTool) Name() string { return "send_location" }

func (t *sendLocationTool) Description() string {
	return `Send a geographic point or venue location to the current chat.`
}

func (t *sendLocationTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"latitude": map[string]any{
				"type":        "number",
				"description": "Latitude of the location (-90 to 90).",
			},
			"longitude": map[string]any{
				"type":        "number",
				"description": "Longitude of the location (-180 to 180).",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Name of the venue (optional, sends as venue if provided).",
			},
			"address": map[string]any{
				"type":        "string",
				"description": "Address of the venue (optional).",
			},
		},
		"required": []string{"latitude", "longitude"},
	}
}

func (t *sendLocationTool) Execute(_ context.Context, args map[string]any) *toolshared.ToolResult {
	lat, okLat := args["latitude"].(float64)
	lon, okLon := args["longitude"].(float64)
	if !okLat || !okLon {
		return toolshared.ErrorResult("latitude and longitude numbers are required")
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return toolshared.ErrorResult(fmt.Sprintf("coordinates out of range: lat=%f lon=%f", lat, lon))
	}

	title, _ := args["title"].(string)
	address, _ := args["address"].(string)

	payload := &bus.LocationPayload{
		Latitude:  lat,
		Longitude: lon,
		Title:     strings.TrimSpace(title),
		Address:   strings.TrimSpace(address),
	}

	return &toolshared.ToolResult{
		ForLLM:          "Location queued for delivery.",
		ResponseHandled: true,
		Location:        payload,
	}
}

// SendContactTool sends contact card information.
type sendContactTool struct{}

func NewSendContactTool() Tool {
	return &sendContactTool{}
}

func (t *sendContactTool) Name() string { return "send_contact" }

func (t *sendContactTool) Description() string {
	return `Send a contact card to the current chat.`
}

func (t *sendContactTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"phone_number": map[string]any{
				"type":        "string",
				"description": "Contact's phone number.",
			},
			"first_name": map[string]any{
				"type":        "string",
				"description": "Contact's first name.",
			},
			"last_name": map[string]any{
				"type":        "string",
				"description": "Contact's last name (optional).",
			},
			"vcard": map[string]any{
				"type":        "string",
				"description": "Additional contact info in vCard format (optional).",
			},
		},
		"required": []string{"phone_number", "first_name"},
	}
}

func (t *sendContactTool) Execute(_ context.Context, args map[string]any) *toolshared.ToolResult {
	phone, _ := args["phone_number"].(string)
	firstName, _ := args["first_name"].(string)
	lastName, _ := args["last_name"].(string)
	vcard, _ := args["vcard"].(string)

	phone = strings.TrimSpace(phone)
	firstName = strings.TrimSpace(firstName)
	if phone == "" || firstName == "" {
		return toolshared.ErrorResult("phone_number and first_name are required")
	}

	payload := &bus.ContactPayload{
		PhoneNumber: phone,
		FirstName:   firstName,
		LastName:    strings.TrimSpace(lastName),
		VCard:       strings.TrimSpace(vcard),
	}

	return &toolshared.ToolResult{
		ForLLM:          "Contact card queued for delivery.",
		ResponseHandled: true,
		Contact:         payload,
	}
}

// SendDiceTool rolls an animated dice in the chat.
type sendDiceTool struct{}

func NewSendDiceTool() Tool {
	return &sendDiceTool{}
}

func (t *sendDiceTool) Name() string { return "send_dice" }

func (t *sendDiceTool) Description() string {
	return `Roll an animated dice or random value in the chat. Emoji can be 🎲, 🎯, 🏀, ⚽, 🎳, or 🎰.`
}

func (t *sendDiceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"emoji": map[string]any{
				"type":        "string",
				"description": "Dice emoji: 🎲 (default), 🎯, 🏀, ⚽, 🎳, or 🎰.",
			},
		},
	}
}

var validDiceEmojis = map[string]bool{
	"🎲": true,
	"🎯": true,
	"🏀": true,
	"⚽": true,
	"🎳": true,
	"🎰": true,
}

func (t *sendDiceTool) Execute(_ context.Context, args map[string]any) *toolshared.ToolResult {
	emoji, _ := args["emoji"].(string)
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		emoji = "🎲"
	}
	if !validDiceEmojis[emoji] {
		return toolshared.ErrorResult("invalid dice emoji; must be one of 🎲, 🎯, 🏀, ⚽, 🎳, 🎰")
	}

	return &toolshared.ToolResult{
		ForLLM:          fmt.Sprintf("Dice roll (%s) queued for delivery.", emoji),
		ResponseHandled: true,
		Dice:            &bus.DicePayload{Emoji: emoji},
	}
}
