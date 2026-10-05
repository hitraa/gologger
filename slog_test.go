//go:build go1.21

package gologger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestSlogHandlerPreservesGroupsAndAttributes(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{Output: &output, Format: FormatJSON, Color: ColorNever})
	if err != nil {
		t.Fatal(err)
	}
	slogLogger := slog.New(logger.SlogHandler()).With("service", "vehicle-api").WithGroup("request")
	slogLogger.Info("connected", "vehicle_id", 17)
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode slog output: %v", err)
	}
	if record["service"] != "vehicle-api" || record["request.vehicle_id"] != float64(17) || record["msg"] != "connected" {
		t.Fatalf("unexpected slog record: %#v", record)
	}
	if source, ok := record["source"].(string); !ok || !strings.Contains(source, "slog_test.go:") {
		t.Fatalf("slog source location missing: %#v", record["source"])
	}
}

func TestSlogHandlerRespectsLoggerThreshold(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{Level: LevelWarn, Output: &output})
	if err != nil {
		t.Fatal(err)
	}
	handler := logger.SlogHandler()
	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("INFO should be disabled by WARN threshold")
	}
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Fatal("WARN should be enabled")
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
}
