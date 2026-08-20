package commands

func useCommand() Definition {
	return Definition{
		Name:        "use",
		Description: "Open the skill picker or force an installed skill for a request",
		Usage:       "/use [skill] [message]",
	}
}
