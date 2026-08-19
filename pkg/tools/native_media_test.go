package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSendLocationTool(t *testing.T) {
	tool := NewSendLocationTool()
	assert.Equal(t, "send_location", tool.Name())

	// Missing coordinates
	res := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, res.IsError)

	// Out of range coordinates
	res = tool.Execute(context.Background(), map[string]any{
		"latitude":  95.0,
		"longitude": 200.0,
	})
	assert.True(t, res.IsError)

	// Valid point
	res = tool.Execute(context.Background(), map[string]any{
		"latitude":  -6.2088,
		"longitude": 106.8456,
	})
	assert.False(t, res.IsError)
	assert.True(t, res.ResponseHandled)
	assert.NotNil(t, res.Location)
	assert.Equal(t, -6.2088, res.Location.Latitude)
	assert.Equal(t, 106.8456, res.Location.Longitude)

	// Valid venue
	res = tool.Execute(context.Background(), map[string]any{
		"latitude":  -6.2088,
		"longitude": 106.8456,
		"title":     "National Monument",
		"address":   "Central Jakarta",
	})
	assert.False(t, res.IsError)
	assert.Equal(t, "National Monument", res.Location.Title)
	assert.Equal(t, "Central Jakarta", res.Location.Address)
}

func TestSendContactTool(t *testing.T) {
	tool := NewSendContactTool()
	assert.Equal(t, "send_contact", tool.Name())

	// Missing phone or name
	res := tool.Execute(context.Background(), map[string]any{
		"first_name": "Alice",
	})
	assert.True(t, res.IsError)

	// Valid contact
	res = tool.Execute(context.Background(), map[string]any{
		"phone_number": "+62812345678",
		"first_name":   "Alice",
		"last_name":    "Smith",
	})
	assert.False(t, res.IsError)
	assert.True(t, res.ResponseHandled)
	assert.NotNil(t, res.Contact)
	assert.Equal(t, "+62812345678", res.Contact.PhoneNumber)
	assert.Equal(t, "Alice", res.Contact.FirstName)
	assert.Equal(t, "Smith", res.Contact.LastName)
}

func TestSendDiceTool(t *testing.T) {
	tool := NewSendDiceTool()
	assert.Equal(t, "send_dice", tool.Name())

	// Default emoji
	res := tool.Execute(context.Background(), map[string]any{})
	assert.False(t, res.IsError)
	assert.True(t, res.ResponseHandled)
	assert.NotNil(t, res.Dice)
	assert.Equal(t, "🎲", res.Dice.Emoji)

	// Valid emoji
	for _, emoji := range []string{"🎲", "🎯", "🏀", "⚽", "🎳", "🎰"} {
		res = tool.Execute(context.Background(), map[string]any{"emoji": emoji})
		assert.False(t, res.IsError)
		assert.Equal(t, emoji, res.Dice.Emoji)
	}

	// Invalid emoji
	res = tool.Execute(context.Background(), map[string]any{"emoji": "🚀"})
	assert.True(t, res.IsError)
}
