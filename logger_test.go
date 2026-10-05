package gologger

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

func TestGenericLogMethodsReportTheirCallSite(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{Output: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	_, _, logLine, _ := runtime.Caller(0)
	if err := logger.Log(LevelInfo, "direct log"); err != nil {
		t.Fatal(err)
	}
	_, _, logfLine, _ := runtime.Caller(0)
	if err := logger.Logf(LevelInfo, "%s", "direct logf"); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("(logger_test.go:%d)", logLine+1)
	if !strings.Contains(output.String(), want) {
		t.Fatalf("Log source does not point to its call site %q: %q", want, output.String())
	}
	want = fmt.Sprintf("(logger_test.go:%d)", logfLine+1)
	if !strings.Contains(output.String(), want) {
		t.Fatalf("Logf source does not point to its call site %q: %q", want, output.String())
	}
}

func TestFilteredLogDoesNotFormatArguments(t *testing.T) {
	logger, err := New(Config{Level: LevelInfo, Output: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	formatted := 0
	value := countedStringer{formatted: &formatted}
	if err := logger.Debug(value); err != nil {
		t.Fatal(err)
	}
	if err := logger.Debugf("%v", value); err != nil {
		t.Fatal(err)
	}
	if formatted != 0 {
		t.Fatalf("filtered values were formatted %d times", formatted)
	}
}

func TestDefaultOutputAndOptionalFile(t *testing.T) {
	logger, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if logger.state.output != os.Stdout {
		t.Fatal("nil output should default to stdout")
	}
	if logger.state.file != nil {
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

func TestMessageEscapesTerminalControlCharacters(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{Output: &output, Color: ColorNever})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Info("untrusted\x1b[2J\x07\u0085"); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b") || strings.Contains(output.String(), "\x07") {
		t.Fatalf("raw terminal control character escaped into output: %q", output.String())
	}
	if !strings.Contains(output.String(), `untrusted\u001B[2J\u0007\u0085`) {
		t.Fatalf("control characters were not visibly escaped: %q", output.String())
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

func TestWithFieldsSharesOutputAndHasIndependentLevel(t *testing.T) {
	var output bytes.Buffer
	base, err := New(Config{Level: LevelDebug, Output: &output, Color: ColorNever})
	if err != nil {
		t.Fatal(err)
	}
	child := base.With(Field{Key: "service", Value: "vehicle-api"})
	if err := child.SetLevel(LevelWarn); err != nil {
		t.Fatal(err)
	}
	if err := base.Debug("base debug"); err != nil {
		t.Fatal(err)
	}
	if err := child.Info("filtered child info"); err != nil {
		t.Fatal(err)
	}
	if err := child.Warn("child warning"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "base debug") ||
		strings.Contains(output.String(), "filtered child info") ||
		!strings.Contains(output.String(), `service="vehicle-api"`) ||
		!strings.Contains(output.String(), "child warning") {
		t.Fatalf("unexpected contextual logger output: %q", output.String())
	}
	if base.Level() != LevelDebug || child.Level() != LevelWarn {
		t.Fatalf("base and child levels were not independent: %v / %v", base.Level(), child.Level())
	}
	if err := base.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJSONFormatIncludesContextAndRecordFields(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{
		Output: &output,
		Color:  ColorAlways,
		Format: FormatJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	contextual := logger.With(Field{Key: "service", Value: "vehicle-api"})
	if err := contextual.LogFields(LevelInfo, "connected", Field{Key: "vehicle_id", Value: 17}); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\033[") {
		t.Fatalf("JSON output must not contain terminal color escapes: %q", output.String())
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode JSON log record: %v", err)
	}
	for key, want := range map[string]string{
		"level": "INFO", "msg": "connected", "source": "logger_test.go",
		"service": "vehicle-api",
	} {
		if got, ok := record[key].(string); !ok || (key == "source" && !strings.HasPrefix(got, want)) || (key != "source" && got != want) {
			t.Errorf("field %q = %v, want %q", key, record[key], want)
		}
	}
	if record["vehicle_id"] != float64(17) {
		t.Errorf("vehicle_id = %v, want 17", record["vehicle_id"])
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", record["time"].(string)); err != nil {
		t.Errorf("invalid JSON timestamp: %v", err)
	}
}

func TestStructuredFieldsRejectReservedAndInvalidKeys(t *testing.T) {
	logger, err := New(Config{Output: io.Discard, Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	for _, field := range []Field{
		{Key: "level", Value: "spoofed"},
		{Key: "request id", Value: "invalid key"},
	} {
		if err := logger.LogFields(LevelInfo, "message", field); err == nil {
			t.Errorf("expected invalid field error for key %q", field.Key)
		}
	}
}

func TestSamplingIsPerLevelAndLeavesErrorsUnsampled(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{
		Level:  LevelDebug,
		Output: &output,
		Sampling: &SamplingConfig{
			Initial:    2,
			Thereafter: 3,
			Interval:   time.Hour,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 10; index++ {
		if err := logger.Debugf("debug record %d", index); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < 2; index++ {
		if err := logger.Errorf("error record %d", index); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(output.String(), "[DEBUG]"); got != 5 {
		t.Errorf("got %d sampled DEBUG records, want 5", got)
	}
	if got := strings.Count(output.String(), "[ERROR]"); got != 2 {
		t.Errorf("got %d ERROR records, want unsampled 2", got)
	}
}

func TestSamplingConfigRequiresValidWindow(t *testing.T) {
	_, err := New(Config{
		Output: io.Discard,
		Sampling: &SamplingConfig{
			Initial:    1,
			Thereafter: 1,
		},
	})
	if err == nil {
		t.Fatal("expected zero sampling interval to be rejected")
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

func TestFilePathIsExclusivelyLockedUntilClose(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("exclusive file locks are not implemented on this platform")
	}
	path := filepath.Join(t.TempDir(), "service.log")
	config := Config{Output: io.Discard, File: &FileConfig{Path: path}}
	first, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(config); !errors.Is(err, ErrFileInUse) {
		t.Fatalf("second logger error = %v, want ErrFileInUse", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := New(config)
	if err != nil {
		t.Fatalf("logger could not reopen the path after close: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpeningUnterminatedFileSeparatesNextRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	if err := os.WriteFile(path, []byte("previous record without newline"), 0600); err != nil {
		t.Fatal(err)
	}
	logger, err := New(Config{
		Output: io.Discard,
		File:   &FileConfig{Path: path, MaxLines: 1, MaxBackups: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Info("new record"); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readLog(t, path+".1"); got != "previous record without newline\n" {
		t.Fatalf("unterminated record was not normalized before rotation: %q", got)
	}
	if got := readLog(t, path); !strings.Contains(got, ") [INFO] new record\n") {
		t.Fatalf("unexpected active file after rotation: %q", got)
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

func TestFileRotationByInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	if err := os.WriteFile(path, []byte("old record\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	logger, err := New(Config{
		Output: io.Discard,
		File: &FileConfig{
			Path:           path,
			RotateInterval: time.Hour,
			MaxBackups:     1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Info("new interval"); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readLog(t, path+".1"); got != "old record\n" {
		t.Fatalf("old active file was not rotated: %q", got)
	}
	if got := readLog(t, path); !strings.Contains(got, "new interval") {
		t.Fatalf("new interval record missing from active file: %q", got)
	}
}

func TestFileAgeRetentionRemovesExpiredBackups(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "service.log")
	oldBackup := path + ".1"
	newBackup := path + ".2"
	for _, backup := range []string{oldBackup, newBackup} {
		if err := os.WriteFile(backup, []byte("backup\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldBackup, old, old); err != nil {
		t.Fatal(err)
	}
	logger, err := New(Config{
		Output: io.Discard,
		File: &FileConfig{
			Path:       path,
			MaxAge:     time.Hour,
			MaxBackups: 2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldBackup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired backup remains: %v", err)
	}
	if _, err := os.Stat(newBackup); err != nil {
		t.Fatalf("recent backup was removed: %v", err)
	}
}

func TestRotationPrunesExpiredActiveFileAfterRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	if err := os.WriteFile(path, []byte("old active record\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	logger, err := New(Config{
		Output: io.Discard,
		File: &FileConfig{
			Path:           path,
			RotateInterval: time.Hour,
			MaxAge:         time.Hour,
			MaxBackups:     1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Info("current record"); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired active file was retained as a backup: %v", err)
	}
	if got := readLog(t, path); !strings.Contains(got, "current record") {
		t.Fatalf("current record missing after pruning old active file: %q", got)
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

func TestSyncSkipsNonRegularFilesAndSyncsRegularFiles(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	pipeLogger, err := New(Config{Output: writer})
	if err != nil {
		t.Fatal(err)
	}
	if err := pipeLogger.Sync(); err != nil {
		t.Fatalf("Sync should not fail for pipe output: %v", err)
	}
	if err := pipeLogger.Close(); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	_ = reader.Close()

	regularFile, err := os.CreateTemp(t.TempDir(), "output-*.log")
	if err != nil {
		t.Fatal(err)
	}
	regularLogger, err := New(Config{Output: regularFile})
	if err != nil {
		_ = regularFile.Close()
		t.Fatal(err)
	}
	if err := regularLogger.Sync(); err != nil {
		t.Errorf("Sync failed for regular file output: %v", err)
	}
	if err := regularLogger.Close(); err != nil {
		t.Fatal(err)
	}
	if err := regularFile.Close(); err != nil {
		t.Fatal(err)
	}
}

var errWriter = errors.New("writer failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriter }

type countedStringer struct {
	formatted *int
}

func (value countedStringer) String() string {
	(*value.formatted)++
	return "formatted"
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
