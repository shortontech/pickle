package cooked

import (
	"fmt"
	"os"
)

// Command represents a CLI subcommand that the compiled binary can run.
type Command interface {
	Name() string
	Description() string
	Run(args []string) error
}

// CommandRegistry holds registered commands and dispatches them.
type CommandRegistry struct {
	commands map[string]Command
}

// NewCommandRegistry creates an empty command registry.
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{commands: map[string]Command{}}
}

// Register adds a command to the registry.
func (r *CommandRegistry) Register(cmd Command) {
	r.commands[cmd.Name()] = cmd
}

// HasCommand returns true if args[0] matches a registered command.
func (r *CommandRegistry) HasCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	_, ok := r.commands[args[0]]
	return ok
}

// Dispatch runs the command matching args[0], passing the remaining args.
// Returns an error if the command is not found or if Run fails.
func (r *CommandRegistry) Dispatch(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command specified")
	}
	cmd, ok := r.commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown command: %s", args[0])
	}
	return cmd.Run(args[1:])
}

// PrintCommands prints available commands to stderr.
func (r *CommandRegistry) PrintCommands() {
	fmt.Fprintln(os.Stderr, "Available commands:")
	for name, cmd := range r.commands {
		fmt.Fprintf(os.Stderr, "  %-25s %s\n", name, cmd.Description())
	}
}
