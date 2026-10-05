//go:build go1.21

package gologger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
)

// SlogHandler returns a standard-library slog.Handler backed by logger.
// It is available when building with Go 1.21 or newer.
func (logger *Logger) SlogHandler() slog.Handler {
	return &slogHandler{logger: logger}
}

type slogHandler struct {
	logger *Logger
	attrs  []Field
	groups []string
	err    error
}

func (handler *slogHandler) Enabled(_ context.Context, level slog.Level) bool {
	threshold := Level(handler.logger.level.Load())
	return threshold != LevelOff && slogLevel(level) >= threshold
}

func (handler *slogHandler) Handle(_ context.Context, record slog.Record) error {
	if handler.err != nil {
		return handler.err
	}
	fields := append([]Field(nil), handler.attrs...)
	var attrErr error
	record.Attrs(func(attribute slog.Attr) bool {
		fields, attrErr = appendSlogAttribute(fields, handler.groups, attribute)
		return attrErr == nil
	})
	if attrErr != nil {
		return attrErr
	}
	source := ""
	if record.PC != 0 {
		frames := runtime.CallersFrames([]uintptr{record.PC})
		frame, _ := frames.Next()
		if frame.File != "" {
			source = fmt.Sprintf("%s:%d", filepath.Base(frame.File), frame.Line)
		}
	}
	metadata := &recordMetadata{
		timestamp: record.Time,
		source:    source,
		sourceSet: true,
	}
	return handler.logger.write(slogLevel(record.Level), 0, func() string { return record.Message }, fields, metadata)
}

func (handler *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	child := handler.clone()
	for _, attribute := range attrs {
		fields, err := appendSlogAttribute(child.attrs, child.groups, attribute)
		if err != nil {
			child.err = errors.Join(child.err, err)
			continue
		}
		child.attrs = fields
	}
	return child
}

func (handler *slogHandler) WithGroup(name string) slog.Handler {
	child := handler.clone()
	if name != "" {
		child.groups = append(child.groups, name)
	}
	return child
}

func (handler *slogHandler) clone() *slogHandler {
	return &slogHandler{
		logger: handler.logger,
		attrs:  append([]Field(nil), handler.attrs...),
		groups: append([]string(nil), handler.groups...),
		err:    handler.err,
	}
}

func slogLevel(level slog.Level) Level {
	switch {
	case level <= slog.LevelDebug:
		return LevelDebug
	case level <= slog.LevelInfo:
		return LevelInfo
	case level <= slog.LevelWarn:
		return LevelWarn
	default:
		return LevelError
	}
}

func appendSlogAttribute(fields []Field, groups []string, attribute slog.Attr) ([]Field, error) {
	attribute.Value = attribute.Value.Resolve()
	if attribute.Value.Kind() == slog.KindGroup {
		nestedGroups := append(append([]string(nil), groups...), attribute.Key)
		if attribute.Key == "" {
			nestedGroups = groups
		}
		for _, nested := range attribute.Value.Group() {
			var err error
			fields, err = appendSlogAttribute(fields, nestedGroups, nested)
			if err != nil {
				return nil, err
			}
		}
		return fields, nil
	}
	keyParts := append(append([]string(nil), groups...), attribute.Key)
	keyParts = compactGroupNames(keyParts)
	if len(keyParts) == 0 {
		return nil, errors.New("gologger: slog attribute has no key")
	}
	key := strings.Join(keyParts, ".")
	if !validFieldKey(key) {
		return nil, fmt.Errorf("gologger: invalid slog attribute key %q", key)
	}
	return append(fields, Field{Key: key, Value: attribute.Value.Any()}), nil
}

func compactGroupNames(names []string) []string {
	compacted := names[:0]
	for _, name := range names {
		if name != "" {
			compacted = append(compacted, name)
		}
	}
	return compacted
}
