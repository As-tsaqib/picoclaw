package commands

func useCommand() Definition {
	return Definition{
		Name:        "use",
		Description: "Pick, arm, clear, or force an installed skill",
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
