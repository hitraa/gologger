package gologger

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
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

// FileConfig configures append-only file output and optional size/line rotation.
// A non-positive MaxBytes or MaxLines disables that rotation limit. Backups are
// named by appending .1, .2, and so on to Path.
type FileConfig struct {
	// Path is the required log file path.
	Path string

	// MaxBytes rotates before a record would exceed this many bytes. Zero disables the limit.
	MaxBytes int64
	// MaxLines rotates before a record would exceed this many lines. Zero disables the limit.
	MaxLines int64

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
	// DisableSource omits the source file and line when true. Sources are on by default.
	DisableSource bool
}

// Logger writes serialized log records to one or more destinations.
// Its zero value is not ready for use; create one with New.
type Logger struct {
	mu        sync.Mutex
	level     Level
	output    io.Writer
	file      *rotatingFile
	addSource bool
	color     bool
	closed    bool
}

// ErrClosed is returned when an operation writes to or syncs a closed Logger.
var ErrClosed = errors.New("logger: logger is closed")

var messageEscaper = strings.NewReplacer("\r\n", `\n`, "\n", `\n`, "\r", `\r`)

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
	if config.Output == nil {
		config.Output = os.Stdout
	}

	logger := &Logger{
		level:     config.Level,
		output:    config.Output,
		addSource: !config.DisableSource,
		color:     useColor(config.Color, config.Output),
	}
	if config.File != nil {
		file, err := openRotatingFile(*config.File)
		if err != nil {
			return nil, err
		}
		logger.file = file
	}
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
		if !ok {
			return false
		}
		info, err := file.Stat()
		return err == nil && info.Mode()&os.ModeCharDevice != 0
	}
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

// SetLevel changes the minimum severity written by the logger.
func (logger *Logger) SetLevel(level Level) error {
	if !validThreshold(level) {
		return fmt.Errorf("logger: invalid level %d", level)
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return ErrClosed
	}
	logger.level = level
	return nil
}

// Level returns the current minimum severity.
func (logger *Logger) Level() Level {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return logger.level
}

// Log writes a record at level. Messages are kept on one physical line.
func (logger *Logger) Log(level Level, args ...any) error {
	return logger.write(level, fmt.Sprint(args...))
}

// Logf writes a formatted record at level.
func (logger *Logger) Logf(level Level, format string, args ...any) error {
	return logger.write(level, fmt.Sprintf(format, args...))
}

// Debug writes a DEBUG record.
func (logger *Logger) Debug(args ...any) error { return logger.Log(LevelDebug, args...) }

// Debugf writes a formatted DEBUG record.
func (logger *Logger) Debugf(format string, args ...any) error {
	return logger.Logf(LevelDebug, format, args...)
}

// Info writes an INFO record.
func (logger *Logger) Info(args ...any) error { return logger.Log(LevelInfo, args...) }

// Infof writes a formatted INFO record.
func (logger *Logger) Infof(format string, args ...any) error {
	return logger.Logf(LevelInfo, format, args...)
}

// Warn writes a WARN record.
func (logger *Logger) Warn(args ...any) error { return logger.Log(LevelWarn, args...) }

// Warnf writes a formatted WARN record.
func (logger *Logger) Warnf(format string, args ...any) error {
	return logger.Logf(LevelWarn, format, args...)
}

// Error writes an ERROR record.
func (logger *Logger) Error(args ...any) error { return logger.Log(LevelError, args...) }

// Errorf writes a formatted ERROR record.
func (logger *Logger) Errorf(format string, args ...any) error {
	return logger.Logf(LevelError, format, args...)
}

func (logger *Logger) write(level Level, message string) error {
	if !validLevel(level) {
		return fmt.Errorf("logger: invalid level %d", level)
	}

	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return ErrClosed
	}
	if logger.level == LevelOff || level < logger.level {
		return nil
	}

	message = messageEscaper.Replace(message)
	var record strings.Builder
	record.WriteByte('[')
	record.WriteString(time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	record.WriteString("] ")
	if logger.addSource {
		_, source, line, ok := runtime.Caller(3)
		if ok {
			record.WriteByte('(')
			record.WriteString(filepath.Base(source))
			record.WriteByte(':')
			record.WriteString(fmt.Sprint(line))
			record.WriteString(") ")
		}
	}
	record.WriteByte('[')
	record.WriteString(level.String())
	record.WriteString("] ")
	record.WriteString(message)
	record.WriteByte('\n')
	data := []byte(record.String())

	var outputErr error
	if logger.output != nil {
		outputData := data
		if logger.color {
			outputData = colorize(level, data)
		}
		if err := writeAll(logger.output, outputData); err != nil {
			outputErr = fmt.Errorf("logger: write output: %w", err)
		}
	}
	if logger.file != nil {
		if err := logger.file.WriteRecord(data); err != nil {
			outputErr = errors.Join(outputErr, fmt.Errorf("logger: write file: %w", err))
		}
	}
	return outputErr
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

// Sync asks destinations that support syncing to flush their data to stable
// storage. Writers without a Sync method are left unchanged.
func (logger *Logger) Sync() error {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return ErrClosed
	}
	var syncErr error
	if destination, ok := logger.output.(interface{ Sync() error }); ok {
		syncErr = destination.Sync()
	}
	if logger.file != nil {
		syncErr = errors.Join(syncErr, logger.file.Sync())
	}
	return syncErr
}

// Close closes the logger's file output. It does not close the configured
// Output writer. Close is safe to call more than once.
func (logger *Logger) Close() error {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return nil
	}
	logger.closed = true
	if logger.file == nil {
		return nil
	}
	err := logger.file.Close()
	logger.file = nil
	return err
}
