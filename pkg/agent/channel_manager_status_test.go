package agent

// GetStatus keeps the recordingChannelManager test double aligned with the
// production read-only ChannelManager status contract. This double does not
// model registered channels, so its truthful status snapshot is empty.
func (m *recordingChannelManager) GetStatus() map[string]any {
	return map[string]any{}
}
