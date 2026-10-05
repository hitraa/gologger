package main

import (
	"errors"
	"fmt"
	stdlog "log"
	"time"

	"github.com/hitraa/gologger"
)

func main() {
	if err := run(); err != nil {
		stdlog.Fatal(err)
	}
}

func run() (returnErr error) {
	level, err := gologger.ParseLevel("debug")
	if err != nil {
		return fmt.Errorf("parse log level: %w", err)
	}
	logFile := &gologger.FileConfig{
		Path:           "logs/service.log",
		MaxBytes:       10 << 20,
		MaxLines:       100_000,
		RotateInterval: 24 * time.Hour,
		MaxAge:         7 * 24 * time.Hour,
		MaxBackups:     5,
		CreateDirs:     true,
		FileMode:       0600,
	}
	appLogger, err := gologger.New(gologger.Config{
		Level: level,
		Color: gologger.ColorAuto,
		File:  logFile,
	})
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, appLogger.Close())
	}()

	if err := appLogger.Debugf("connecting vehicle id=%d", 17); err != nil {
		return fmt.Errorf("write debug record: %w", err)
	}
	if err := appLogger.SetLevel(gologger.LevelInfo); err != nil {
		return fmt.Errorf("change log level: %w", err)
	}
	if err := appLogger.Infof("vehicle %d connected", 17); err != nil {
		return fmt.Errorf("write info record: %w", err)
	}
	if err := appLogger.Warn("telemetry delay detected"); err != nil {
		return fmt.Errorf("write warning record: %w", err)
	}
	if err := appLogger.Errorf("vehicle heartbeat timed out after %d seconds", 3); err != nil {
		return fmt.Errorf("write error record: %w", err)
	}
	if err := appLogger.Sync(); err != nil {
		return fmt.Errorf("sync logger: %w", err)
	}
	return nil
}
