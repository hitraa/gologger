//go:build go1.21

package main

import (
	"errors"
	stdlog "log"
	"log/slog"

	"github.com/hitraa/gologger"
)

func main() {
	if err := run(); err != nil {
		stdlog.Fatal(err)
	}
}

func run() (returnErr error) {
	logger, err := gologger.New(gologger.Config{
		Level:  gologger.LevelInfo,
		Format: gologger.FormatJSON,
	})
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, logger.Close())
	}()

	appLogger := slog.New(logger.SlogHandler()).With("service", "vehicle-api")
	appLogger.WithGroup("request").Info(
		"vehicle connected",
		slog.Int("vehicle_id", 17),
		slog.String("protocol", "mavlink"),
	)
	return logger.Sync()
}
