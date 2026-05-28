package builtin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"

	"github.com/google/shlex"
	"github.com/kpetremann/jackadi/internal/plugin/inventory"
	"github.com/kpetremann/jackadi/sdk"
)

func run(ctx context.Context, args string) (string, error) {
	if strings.TrimSpace(args) == "" {
		return "", errors.New("args must not be empty")
	}

	partitions, err := shlex.Split(args)
	if err != nil {
		return "", fmt.Errorf("failed to partition: %w", err)
	}
	cmd := exec.Command(partitions[0], partitions[1:]...) //nolint:gosec // running external inputs by design
	// prevent signal from canceling the command to avoid unwanter signal propagation
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	res, err := cmd.CombinedOutput() // TODO: cancel if context is closed

	return string(res), err
}

func MustLoadCmd() {
	cmd := sdk.New("cmd")
	cmd.MustRegisterTask("run", run).
		WithSummary("Execute a command.").
		WithDescription("The executed command is not canceled when the node is closed.\nIt leverages exec.Command.").
		WithArg("cmd", "string", "ls -l")

	if err := inventory.Registry.Register(cmd); err != nil {
		name, _ := cmd.Name()
		slog.Error("could not load builtin task", "error", err, "task", name)
		log.Fatal(err)
	}
}
