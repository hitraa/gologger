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
	logger, err := gologger.New(gologger.Config{
		Level: gologger.LevelDebug,
		File: &gologger.FileConfig{
			Path:           "logs/sampled.log",
			MaxBytes:       10 << 20,
			MaxLines:       100_000,
			RotateInterval: 24 * time.Hour,
			MaxAge:         7 * 24 * time.Hour,
			MaxBackups:     5,
			CreateDirs:     true,
			FileMode:       0600,
		},
		Sampling: &gologger.SamplingConfig{
			Initial:    3,
			Thereafter: 100,
			Interval:   time.Minute,
		},
	})
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, logger.Close())
	}()

	for index := 0; index < 250; index++ {
		if err := logger.Debugf("poll attempt %d", index); err != nil {
			return fmt.Errorf("write sampled debug record: %w", err)
		}
	}
	if err := logger.Error("vehicle connection lost"); err != nil {
		return fmt.Errorf("write unsampled error record: %w", err)
	}
	return logger.Sync()
}
