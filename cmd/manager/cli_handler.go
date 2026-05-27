package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/kpetremann/jackadi/internal/config"
)

type closeFunc func()

func NewCLIListener() (net.Listener, closeFunc, error) {
	_ = os.MkdirAll(filepath.Dir(config.CLISocket), 0755)

	listener, err := net.Listen("unix", config.CLISocket)
	if err != nil {
		return nil, func() {}, fmt.Errorf("failed to connect to CLI socket: %w", err)
	}

	closeFunc := func() {
		_ = listener.Close()
	}

	if err := os.Chmod(config.CLISocket, 0700); err != nil {
		closeFunc() // for close to avoid users forgetting to do so because of the error
		return nil, func() {}, fmt.Errorf("failed to secure CLI socket: %w", err)
	}

	return listener, closeFunc, nil
}
