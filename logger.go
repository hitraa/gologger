package gologger

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// Level controls which log records are written. Higher severity levels pass a
// threshold with the same or lower severity. LevelOff disables all records.
type Level uint8

const (
	// LevelUnset selects the default INFO threshold in Config.
	LevelUnset Level = iota
	// LevelDebug records diagnostic details.
	LevelDebug
	// LevelInfo records routine operational events.
	LevelInfo
	// LevelWarn records recoverable or unexpected events.
	LevelWarn
	// LevelError records failures requiring attention.
	LevelError
	// LevelOff disables all log records.
	LevelOff
)

// String returns the level's uppercase name.
func (level Level) String() string {
	switch level {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelOff:
		return "OFF"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel parses a level name without regard to case.
func ParseLevel(value string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	case "off", "none":
		return LevelOff, nil
	default:
		return LevelUnset, fmt.Errorf("logger: invalid level %q", value)
	}
}

// ColorMode controls ANSI color on the configured output writer. File output
// is always uncolored.
type ColorMode uint8

const (
	// ColorAuto colors output only when it is a terminal *os.File.
	ColorAuto ColorMode = iota
	// ColorAlways colors output regardless of the writer type.
	ColorAlways
	// ColorNever disables ANSI color.
	ColorNever
)

// Format selects the representation used for each log record.
type Format uint8

const (
	// FormatText writes the human-readable default format.
	FormatText Format = iota
	// FormatJSON writes one JSON object per line.
	FormatJSON
)

// Field is a structured key/value attribute attached to a log record.
// Keys must contain only letters, digits, '.', '-', or '_'.
type Field struct {
	Key   string
	Value any
}

// SamplingConfig limits records per severity within a fixed time window.
// The first Initial records pass; afterward every Thereafter-th record passes.
// Errors bypass sampling unless SampleErrors is true. Sampling is disabled
// unless Config.Sampling is set.
type SamplingConfig struct {
	Initial      int           // Records to always retain per level and interval.
	Thereafter   int           // Retain one of each N later records; zero disables later records.
	Interval     time.Duration // Counter reset interval; must be positive.
	SampleErrors bool          // Apply sampling to ERROR records when true.
}

// FileConfig configures append-only file output and optional size, line, time,
// and age-based rotation/retention. Backups are named by appending .1, .2, and
// so on to Path. Linux, macOS, and Windows file sinks hold an exclusive lock at
// Path + ".lock" for their lifetime.
type FileConfig struct {
	// Path is the required log file path.
	Path string

	// MaxBytes rotates before a record would exceed this many bytes. Zero disables the limit.
	MaxBytes int64
	// MaxLines rotates before a record would exceed this many lines. Zero disables the limit.
	MaxLines int64
	// RotateInterval rotates the active file after this duration; zero disables time rotation.
	RotateInterval time.Duration
	// MaxAge removes numbered backups older than this duration; zero disables age-based cleanup.
	MaxAge time.Duration

	// MaxBackups is the number of numbered backups to retain; zero truncates on rotation.
	MaxBackups int
	// CreateDirs creates missing parent directories when true.
	CreateDirs bool
	// FileMode sets permissions for new files; zero uses 0640.
	FileMode os.FileMode
}

// Config configures a Logger. A nil Output uses os.Stdout. File is optional;
// when set, records are written to both Output and the file. Source locations
// are included by default, and ColorAuto enables color only for terminal output.
type Config struct {
	// Level is the minimum severity; LevelUnset defaults to LevelInfo.
	Level Level
	// Output receives console/application output; nil defaults to os.Stdout.
	Output io.Writer
	// File optionally enables a rotating file destination alongside Output.
	File *FileConfig
	// Color controls ANSI color on Output; file records are always plain text.
	Color ColorMode
	// Format selects text or newline-delimited JSON output; zero defaults to FormatText.
	Format Format
	// DisableSource omits the source file and line when true. Sources are on by default.
	DisableSource bool
	// Sampling optionally limits repeated records by severity and time window.
	Sampling *SamplingConfig
}

type loggerState struct {
	mu           sync.Mutex
	output       io.Writer
	file         *rotatingFile
	addSource    bool
	color        bool
	format       Format
	closed       bool
	sampling     *SamplingConfig
	sampleStart  time.Time
	sampleCounts [6]uint64
}

type recordMetadata struct {
	timestamp time.Time
	source    string
	sourceSet bool
}

// Logger writes serialized log records to one or more destinations.
// Its zero value is not ready for use; create one with New.
type Logger struct {
	state  *loggerState
	level  atomic.Uint32
	fields []Field
}

// ErrClosed is returned when an operation writes to or syncs a closed Logger.
var ErrClosed = errors.New("logger: logger is closed")

// ErrFileInUse is returned when another logger already owns the configured log path.
var ErrFileInUse = errors.New("gologger: log file is already in use")

// New creates a logger. The default level is INFO and the default output is
// os.Stdout. File output is optional and can be combined with another writer.
func New(config Config) (*Logger, error) {
	if config.Level == LevelUnset {
		config.Level = LevelInfo
	}
	if !validThreshold(config.Level) {
		return nil, fmt.Errorf("logger: invalid level %d", config.Level)
	}
	if config.Color > ColorNever {
		return nil, fmt.Errorf("logger: invalid color mode %d", config.Color)
	}
	if config.Format > FormatJSON {
		return nil, fmt.Errorf("logger: invalid format %d", config.Format)
	}
	if config.Sampling != nil && (config.Sampling.Initial < 0 || config.Sampling.Thereafter < 0 || config.Sampling.Interval <= 0 || (config.Sampling.Initial == 0 && config.Sampling.Thereafter == 0)) {
		return nil, errors.New("logger: sampling requires non-negative counts, a positive interval, and a non-zero sampling count")
	}
	if config.Output == nil {
		config.Output = os.Stdout
	}

	state := &loggerState{
		output:      config.Output,
		addSource:   !config.DisableSource,
		color:       config.Format == FormatText && useColor(config.Color, config.Output),
		format:      config.Format,
		sampleStart: time.Now(),
	}
	if config.Sampling != nil {
		sampling := *config.Sampling
		state.sampling = &sampling
	}
	if config.File != nil {
		file, err := openRotatingFile(*config.File)
		if err != nil {
			return nil, err
		}
		state.file = file
	}
	logger := &Logger{state: state}
	logger.level.Store(uint32(config.Level))
	return logger, nil
}

func validLevel(level Level) bool {
	return level >= LevelDebug && level <= LevelError
}

func validThreshold(level Level) bool {
	return validLevel(level) || level == LevelOff
}

func useColor(mode ColorMode, output io.Writer) bool {
	switch mode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	default:
		file, ok := output.(*os.File)
		return ok && isTerminal(file)
	}
}

func escapeMessage(message string) string {
	var escaped strings.Builder
	escaped.Grow(len(message))
	for _, character := range message {
		switch character {
		case '\n':
			escaped.WriteString(`\n`)
		case '\r':
			escaped.WriteString(`\r`)
		case '\t':
			escaped.WriteString(`\t`)
		default:
			if unicode.IsControl(character) {
				fmt.Fprintf(&escaped, `\u%04X`, character)
			} else {
				escaped.WriteRune(character)
			}
		}
	}
	return escaped.String()
}

func colorCode(level Level) string {
	switch level {
	case LevelError:
		return "\033[31m"
	case LevelWarn:
		return "\033[33m"
	case LevelInfo:
		return "\033[32m"
	case LevelDebug:
		return "\033[36m"
	default:
		return ""
	}
}

func colorize(level Level, record []byte) []byte {
	code := colorCode(level)
	if code == "" {
		return record
	}
	const reset = "\033[0m"
	colored := make([]byte, 0, len(record)+len(code)+len(reset))
	colored = append(colored, code...)
	colored = append(colored, record[:len(record)-1]...)
	colored = append(colored, reset...)
	return append(colored, record[len(record)-1])
}

func (state *loggerState) allowSample(level Level, now time.Time) bool {
	if state.sampling == nil || (level == LevelError && !state.sampling.SampleErrors) {
		return true
	}
	if now.Sub(state.sampleStart) >= state.sampling.Interval {
		state.sampleStart = now
		state.sampleCounts = [6]uint64{}
	}
	count := state.sampleCounts[level]
	state.sampleCounts[level]++
	if count < uint64(state.sampling.Initial) {
		return true
	}
	if state.sampling.Thereafter == 0 {
		return false
	}
	return (count-uint64(state.sampling.Initial))%uint64(state.sampling.Thereafter) == 0
}

// SetLevel changes the minimum severity written by the logger.
func (logger *Logger) SetLevel(level Level) error {
	if !validThreshold(level) {
		return fmt.Errorf("logger: invalid level %d", level)
	}
	logger.state.mu.Lock()
	defer logger.state.mu.Unlock()
	if logger.state.closed {
		return ErrClosed
	}
	logger.level.Store(uint32(level))
	return nil
}

// Level returns the current minimum severity.
func (logger *Logger) Level() Level {
	return Level(logger.level.Load())
}

// With returns a logger that adds fields to every record. The returned logger
// shares output and lifecycle with its parent but has an independent level.
func (logger *Logger) With(fields ...Field) *Logger {
	combined := make([]Field, 0, len(logger.fields)+len(fields))
	combined = append(combined, logger.fields...)
	combined = append(combined, fields...)
	child := &Logger{state: logger.state, fields: combined}
	child.level.Store(logger.level.Load())
	return child
}

// Log writes a record at level. Messages are kept on one physical line.
func (logger *Logger) Log(level Level, args ...any) error {
	return logger.logArgs(level, args)
}

// Logf writes a formatted record at level.
func (logger *Logger) Logf(level Level, format string, args ...any) error {
	return logger.logFormat(level, format, args)
}

// LogFields writes a message with per-record structured fields.
func (logger *Logger) LogFields(level Level, message string, fields ...Field) error {
	return logger.write(level, 2, func() string { return message }, fields, nil)
}

// Debug writes a DEBUG record.
func (logger *Logger) Debug(args ...any) error { return logger.logArgs(LevelDebug, args) }

// Debugf writes a formatted DEBUG record.
func (logger *Logger) Debugf(format string, args ...any) error {
	return logger.logFormat(LevelDebug, format, args)
}

// Info writes an INFO record.
func (logger *Logger) Info(args ...any) error { return logger.logArgs(LevelInfo, args) }

// Infof writes a formatted INFO record.
func (logger *Logger) Infof(format string, args ...any) error {
	return logger.logFormat(LevelInfo, format, args)
}

// Warn writes a WARN record.
func (logger *Logger) Warn(args ...any) error { return logger.logArgs(LevelWarn, args) }

// Warnf writes a formatted WARN record.
func (logger *Logger) Warnf(format string, args ...any) error {
	return logger.logFormat(LevelWarn, format, args)
}

// Error writes an ERROR record.
func (logger *Logger) Error(args ...any) error { return logger.logArgs(LevelError, args) }

// Errorf writes a formatted ERROR record.
func (logger *Logger) Errorf(format string, args ...any) error {
	return logger.logFormat(LevelError, format, args)
}

func (logger *Logger) logArgs(level Level, args []any) error {
	return logger.write(level, 3, func() string { return fmt.Sprint(args...) }, nil, nil)
}

func (logger *Logger) logFormat(level Level, format string, args []any) error {
	return logger.write(level, 3, func() string { return fmt.Sprintf(format, args...) }, nil, nil)
}

func (logger *Logger) write(level Level, callerSkip int, formatMessage func() string, extraFields []Field, metadata *recordMetadata) error {
	if !validLevel(level) {
		return fmt.Errorf("logger: invalid level %d", level)
	}

	logger.state.mu.Lock()
	if logger.state.closed {
		logger.state.mu.Unlock()
		return ErrClosed
	}
	if currentLevel := Level(logger.level.Load()); currentLevel == LevelOff || level < currentLevel || !logger.state.allowSample(level, time.Now()) {
		logger.state.mu.Unlock()
		return nil
	}
	logger.state.mu.Unlock()

	message := formatMessage()

	logger.state.mu.Lock()
	defer logger.state.mu.Unlock()
	if logger.state.closed {
		return ErrClosed
	}
	if currentLevel := Level(logger.level.Load()); currentLevel == LevelOff || level < currentLevel {
		return nil
	}

	loggedAt := time.Now()
	if metadata != nil && !metadata.timestamp.IsZero() {
		loggedAt = metadata.timestamp
	}
	timestamp := loggedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	source := ""
	if logger.state.addSource {
		if metadata != nil && metadata.sourceSet {
			source = metadata.source
		} else {
			_, sourcePath, line, ok := runtime.Caller(callerSkip)
			if ok {
				source = fmt.Sprintf("%s:%d", filepath.Base(sourcePath), line)
			}
		}
	}
	fields := make([]Field, 0, len(logger.fields)+len(extraFields))
	fields = append(fields, logger.fields...)
	fields = append(fields, extraFields...)
	data, err := formatRecord(logger.state.format, timestamp, level, source, message, fields)
	if err != nil {
		return fmt.Errorf("logger: format record: %w", err)
	}

	var outputErr error
	if logger.state.output != nil {
		outputData := data
		if logger.state.color {
			outputData = colorize(level, data)
		}
		if err := writeAll(logger.state.output, outputData); err != nil {
			outputErr = fmt.Errorf("logger: write output: %w", err)
		}
	}
	if logger.state.file != nil {
		if err := logger.state.file.WriteRecord(data); err != nil {
			outputErr = errors.Join(outputErr, fmt.Errorf("logger: write file: %w", err))
		}
	}
	return outputErr
}

func formatRecord(format Format, timestamp string, level Level, source, message string, fields []Field) ([]byte, error) {
	if format == FormatJSON {
		record := make(map[string]any, len(fields)+4)
		record["time"] = timestamp
		record["level"] = level.String()
		record["msg"] = message
		if source != "" {
			record["source"] = source
		}
		for _, field := range fields {
			if !validFieldKey(field.Key) {
				return nil, fmt.Errorf("invalid field key %q", field.Key)
			}
			switch field.Key {
			case "time", "level", "msg", "source":
				return nil, fmt.Errorf("field key %q is reserved", field.Key)
			}
			record[field.Key] = field.Value
		}
		data, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	}

	var record strings.Builder
	record.WriteByte('[')
	record.WriteString(timestamp)
	record.WriteString("] ")
	if source != "" {
		record.WriteByte('(')
		record.WriteString(source)
		record.WriteString(") ")
	}
	record.WriteByte('[')
	record.WriteString(level.String())
	record.WriteString("] ")
	record.WriteString(escapeMessage(message))
	for _, field := range fields {
		if !validFieldKey(field.Key) {
			return nil, fmt.Errorf("invalid field key %q", field.Key)
		}
		encoded, err := json.Marshal(field.Value)
		if err != nil {
			return nil, fmt.Errorf("encode field %q: %w", field.Key, err)
		}
		record.WriteByte(' ')
		record.WriteString(field.Key)
		record.WriteByte('=')
		record.Write(encoded)
	}
	record.WriteByte('\n')
	return []byte(record.String()), nil
}

func validFieldKey(key string) bool {
	if key == "" {
		return false
	}
	for _, character := range key {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '_' && character != '-' && character != '.' {
			return false
		}
	}
	return true
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func syncWriter(writer io.Writer) error {
	if file, ok := writer.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
	}
	if destination, ok := writer.(interface{ Sync() error }); ok {
		return destination.Sync()
	}
	return nil
}

// Sync asks destinations that support syncing to flush their data to stable
// storage. Writers without a Sync method are left unchanged.
func (logger *Logger) Sync() error {
	logger.state.mu.Lock()
	defer logger.state.mu.Unlock()
	if logger.state.closed {
		return ErrClosed
	}
	syncErr := syncWriter(logger.state.output)
	if logger.state.file != nil {
		syncErr = errors.Join(syncErr, logger.state.file.Sync())
	}
	return syncErr
}

// Close closes the logger's file output. It does not close the configured
// Output writer. Close is safe to call more than once.
func (logger *Logger) Close() error {
	logger.state.mu.Lock()
	defer logger.state.mu.Unlock()
	if logger.state.closed {
		return nil
	}
	logger.state.closed = true
	if logger.state.file == nil {
		return nil
	}
	err := logger.state.file.Close()
	logger.state.file = nil
	return err
}
