package commands

func useCommand() Definition {
	return Definition{
		Name:        "use",
		Description: "Open the skill picker or force an installed skill for a request; arm one for the next message or clear it",
		Usage:       "/use [skill] [message]",
		Category:    "Skills",
		Examples: []string{
			"/use",
			"/use coding",
			"/use coding explain this function",
			"/use clear",
			"/use off",
		},
	}
}
