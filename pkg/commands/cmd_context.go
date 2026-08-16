package commands

import (
	"context"
	"fmt"
)

func contextCommand() Definition {
	return Definition{
		Name:        "context",
		Description: "Show current session context and token usage",
		Usage:       "/context",
		Handler: func(_ context.Context, req Request, rt *Runtime) error {
			if rt == nil || rt.GetContextStats == nil {
				return req.Reply(unavailableMsg)
			}
			stats := rt.GetContextStats()
			if stats == nil {
				return req.Reply("No active session context.")
			}
			fallback := formatContextStats(stats)
			remaining := stats.CompressAtTokens - stats.UsedTokens
			if remaining < 0 {
				remaining = 0
			}
			rows := [][]string{
				{"Messages", fmt.Sprintf("%d", stats.MessageCount)},
				{"Used", fmt.Sprintf("~%d / %d tokens", stats.UsedTokens, stats.TotalTokens)},
				{"History", fmt.Sprintf("~%d tokens", stats.HistoryTokens)},
				{"Compress at", fmt.Sprintf("%d tokens", stats.CompressAtTokens)},
				{"Summarize at", fmt.Sprintf("%d tokens", stats.SummarizeAtTokens)},
				{"Compression progress", fmt.Sprintf("%d%%", stats.UsedPercent)},
				{"Remaining", fmt.Sprintf("~%d tokens", remaining)},
			}
			return req.replyStructured(tableContent("Context usage", []string{"Metrik", "Nilai"}, rows, fallback))
		},
	}
}

func formatContextStats(s *ContextStats) string {
	remaining := s.CompressAtTokens - s.UsedTokens
	if remaining < 0 {
		remaining = 0
	}
	usedWindowPercent := s.UsedTokens * 100 / max(s.TotalTokens, 1)
	msg := fmt.Sprintf(
		"Context usage  \nMessages: %d  \nUsed: ~%d / %d tokens (%d%%)  \nHistory: ~%d tokens  \nCompress at: %d tokens  \nSummarize at: %d tokens  \nCompression progress: %d%%  \nRemaining: ~%d tokens",
		s.MessageCount,
		s.UsedTokens,
		s.TotalTokens,
		usedWindowPercent,
		s.HistoryTokens,
		s.CompressAtTokens,
		s.SummarizeAtTokens,
		s.UsedPercent,
		remaining,
	)
	return msg
}
