package gologger

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoggerFiltersFormatsAndEscapes(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{Level: LevelInfo, Output: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	if err := logger.Debug("hidden"); err != nil {
		t.Fatal(err)
	}
	if err := logger.Infof("ready: %s\nnext", "yes"); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "hidden") ||
		!strings.Contains(got, "] (logger_test.go:") ||
		!strings.Contains(got, ") [INFO] ready: yes\\nnext") {
		t.Fatalf("unexpected log output: %q", got)
	}
	if strings.Contains(got, "\033[") {
		t.Fatalf("auto color should not color a non-terminal writer: %q", got)
	}
	line := strings.TrimSuffix(got, "\n")
	timestampEnd := strings.Index(line, "] ")
	if timestampEnd < 0 {
		t.Fatalf("timestamp is not bracketed: %q", got)
	}
	if _, err := time.Parse("[2006-01-02T15:04:05.000Z]", line[:timestampEnd+1]); err != nil {
		t.Fatalf("invalid timestamp in record %q: %v", got, err)
	}
}

func TestDefaultOutputAndOptionalFile(t *testing.T) {
	logger, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if logger.output != os.Stdout {
		t.Fatal("nil output should default to stdout")
	}
	if logger.file != nil {
		t.Fatal("file output should be disabled unless configured")
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestColorsFollowLevelAndFileStaysPlain(t *testing.T) {
	var output bytes.Buffer
	path := filepath.Join(t.TempDir(), "service.log")
	logger, err := New(Config{
		Level:  LevelDebug,
		Output: &output,
		Color:  ColorAlways,
		File:   &FileConfig{Path: path},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, write := range []func() error{
		func() error { return logger.Debug("debug") },
		func() error { return logger.Info("info") },
		func() error { return logger.Warn("warn") },
		func() error { return logger.Error("error") },
	} {
		if err := write(); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"\033[36m[", "\033[32m[", "\033[33m[", "\033[31m[",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("colored output missing %q: %q", want, output.String())
		}
	}
	fileOutput := readLog(t, path)
	if strings.Contains(fileOutput, "\033[") {
		t.Fatalf("file output should be uncolored: %q", fileOutput)
	}
	for _, level := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		if !strings.Contains(fileOutput, ") ["+level+"] ") {
			t.Errorf("file output missing %s record: %q", level, fileOutput)
		}
	}
}

func TestLevelOffDisablesRecords(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{Level: LevelOff, Output: &output})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Error("disabled"); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("LevelOff wrote output: %q", output.String())
	}
	if err := logger.SetLevel(LevelDebug); err != nil {
		t.Fatal(err)
	}
	if err := logger.Debug("enabled"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "enabled") {
		t.Fatalf("logger did not resume after SetLevel: %q", output.String())
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileRotationByLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	logger, err := New(Config{
		Output: io.Discard,
		Level:  LevelDebug,
		File:   &FileConfig{Path: path, MaxLines: 2, MaxBackups: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"one", "two", "three"} {
		if err := logger.Info(message); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	active := readLog(t, path)
	backup := readLog(t, path+".1")
	if !strings.Contains(active, "three") || strings.Contains(active, "one") {
		t.Fatalf("unexpected active log: %q", active)
	}
	if !strings.Contains(backup, "one") || !strings.Contains(backup, "two") {
		t.Fatalf("unexpected first backup: %q", backup)
	}
}

func TestFileRotationBySize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	logger, err := New(Config{
		Output: io.Discard,
		File:   &FileConfig{Path: path, MaxBytes: 1, MaxBackups: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Info("first"); err != nil {
		t.Fatal(err)
	}
	if err := logger.Info("second"); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readLog(t, path+".1"), "first") || !strings.Contains(readLog(t, path), "second") {
		t.Fatal("size limit did not rotate the log")
	}
}

func TestFileRotationRetainsConfiguredBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	logger, err := New(Config{
		Output: io.Discard,
		File:   &FileConfig{Path: path, MaxLines: 1, MaxBackups: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"one", "two", "three", "four"} {
		if err := logger.Info(message); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	for suffix, want := range map[string]string{"": "four", ".1": "three", ".2": "two"} {
		if got := readLog(t, path+suffix); !strings.Contains(got, want) {
			t.Errorf("%s does not contain %q: %q", path+suffix, want, got)
		}
	}
	if _, err := os.Stat(path + ".3"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected third backup: %v", err)
	}
}

func TestLoggerConcurrentWrites(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{Level: LevelDebug, Output: &output})
	if err != nil {
		t.Fatal(err)
	}
	const goroutines = 8
	const records = 25
	var workers sync.WaitGroup
	for worker := 0; worker < goroutines; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for record := 0; record < records; record++ {
				if err := logger.Info("concurrent"); err != nil {
					t.Errorf("write record: %v", err)
					return
				}
			}
		}()
	}
	workers.Wait()
	if got := strings.Count(output.String(), "concurrent"); got != goroutines*records {
		t.Fatalf("got %d records, want %d", got, goroutines*records)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLoggerWriterError(t *testing.T) {
	logger, err := New(Config{Output: failingWriter{}})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	if err := logger.Info("failure"); !errors.Is(err, errWriter) {
		t.Fatalf("got error %v, want %v", err, errWriter)
	}
}

var errWriter = errors.New("writer failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriter }

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
