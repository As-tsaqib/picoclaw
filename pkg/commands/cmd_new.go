package commands

func newCommand() Definition {
	return Definition{
		Name:        "new",
		Description: "Create and activate a new session",
		Usage:       "/new [name]",
		Category:    "Sessions",
		Examples:    []string{"/new", "/new research"},
		Handler:     sessionOperationHandler("new", 1),
	}
}
