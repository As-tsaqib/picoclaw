package agent

// GetStatus keeps the recordingChannelManager test double aligned with the
// production read-only ChannelManager status contract.
func (m *recordingChannelManager) GetStatus() map[string]any {
	status := make(map[string]any)
	for name, channel := range m.channels {
		status[name] = map[string]any{
			"enabled": true,
			"running": channel != nil && channel.IsRunning(),
		}
	}
	return status
}
