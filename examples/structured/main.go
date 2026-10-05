package main

import (
	"errors"
	stdlog "log"

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

	requestLogger := logger.With(
		gologger.Field{Key: "service", Value: "vehicle-api"},
		gologger.Field{Key: "environment", Value: "production"},
	)
	if err := requestLogger.LogFields(
		gologger.LevelInfo,
		"vehicle connected",
		gologger.Field{Key: "vehicle_id", Value: 17},
		gologger.Field{Key: "protocol", Value: "mavlink"},
	); err != nil {
		return err
	}
	return logger.Sync()
}
