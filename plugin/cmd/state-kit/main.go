package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/zhang2580384/sub2api-state-kit/plugin/internal/engine"
	pluginv1 "github.com/zhang2580384/sub2api-state-kit/plugin/internal/pluginapi/v1"
)

func main() {
	e := engine.New()
	defer e.Close()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-signals; e.Close(); os.Exit(0) }()
	pluginv1.Serve(e)
}
