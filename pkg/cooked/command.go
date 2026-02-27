package cooked

import (
	"fmt"
	"log"
	"os"
)

// Command represents a CLI subcommand that the compiled binary can run.
type Command interface {
	Name() string
	Description() string
	Run(args []string) error
}

// App is the application lifecycle manager. It handles initialization,
// command dispatch, and HTTP serving.
type App struct {
	commands map[string]Command
	initFn   func()
	serveFn  func()
}

// BuildApp creates a new App with the given init, serve, and command functions.
func BuildApp(initFn func(), serveFn func(), cmds ...Command) *App {
	a := &App{
		commands: make(map[string]Command),
		initFn:   initFn,
		serveFn:  serveFn,
	}
	for _, cmd := range cmds {
		a.commands[cmd.Name()] = cmd
	}
	return a
}

// Run initializes the app, then either dispatches a command or starts HTTP.
func (a *App) Run(args []string) {
	a.initFn()

	if len(args) > 0 {
		if cmd, ok := a.commands[args[0]]; ok {
			if err := cmd.Run(args[1:]); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	a.serveFn()
}

// PrintCommands prints available commands to stderr.
func (a *App) PrintCommands() {
	fmt.Fprintln(os.Stderr, "Available commands:")
	for name, cmd := range a.commands {
		fmt.Fprintf(os.Stderr, "  %-25s %s\n", name, cmd.Description())
	}
}
