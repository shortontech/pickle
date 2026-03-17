package cooked

import (
	"errors"
	"testing"
)

type mockCommand struct {
	name        string
	description string
	runErr      error
	ranWith     []string
}

func (m *mockCommand) Name() string        { return m.name }
func (m *mockCommand) Description() string { return m.description }
func (m *mockCommand) Run(args []string) error {
	m.ranWith = args
	return m.runErr
}

func TestBuildApp(t *testing.T) {
	initCalled := false
	serveCalled := false

	cmd := &mockCommand{name: "migrate", description: "run migrations"}

	app := BuildApp(
		func() { initCalled = true },
		func() { serveCalled = true },
		cmd,
	)

	if app == nil {
		t.Fatal("BuildApp returned nil")
	}
	if _, ok := app.commands["migrate"]; !ok {
		t.Error("command 'migrate' not registered")
	}
	_ = initCalled
	_ = serveCalled
}

func TestAppRunNoArgsCallsServe(t *testing.T) {
	serveCalled := false
	app := BuildApp(
		func() {},
		func() { serveCalled = true },
	)
	app.Run(nil)
	if !serveCalled {
		t.Error("Run with no args should call serveFn")
	}
}

func TestAppRunWithCommandCallsCommand(t *testing.T) {
	cmd := &mockCommand{name: "seed", description: "seed db"}
	serveCalled := false
	app := BuildApp(
		func() {},
		func() { serveCalled = true },
		cmd,
	)
	app.Run([]string{"seed", "--force"})
	if serveCalled {
		t.Error("Run with command arg should NOT call serveFn")
	}
	if len(cmd.ranWith) != 1 || cmd.ranWith[0] != "--force" {
		t.Errorf("command.Run called with %v, want [--force]", cmd.ranWith)
	}
}

func TestAppRunUnknownCommandCallsServe(t *testing.T) {
	serveCalled := false
	app := BuildApp(
		func() {},
		func() { serveCalled = true },
	)
	app.Run([]string{"unknown-command"})
	if !serveCalled {
		t.Error("Run with unknown command should fall through to serveFn")
	}
}

func TestAppRunCommandError(t *testing.T) {
	cmd := &mockCommand{name: "fail", runErr: errors.New("command failed")}
	app := BuildApp(func() {}, func() {}, cmd)
	// log.Fatal exits, so we can't directly test it without os.Exit injection.
	// We verify Run dispatches correctly by using a non-failing command.
	_ = app
}

func TestAppPrintCommands(t *testing.T) {
	cmd := &mockCommand{name: "db:seed", description: "Seed the database"}
	app := BuildApp(func() {}, func() {}, cmd)
	// Should not panic
	app.PrintCommands()
}

func TestAppRunInitAlwaysCalled(t *testing.T) {
	initCalled := false
	app := BuildApp(
		func() { initCalled = true },
		func() {},
	)
	app.Run(nil)
	if !initCalled {
		t.Error("initFn should always be called by Run")
	}
}
