# gologger

A small Go logging library with no external dependencies. It writes serialized,
UTC-stamped records to an `io.Writer`, an optional rotating log file, or both.

## Install

```sh
go get github.com/hitraa/gologger@v1.0.0
```

Requires Go 1.20 or newer.

gologger follows Semantic Versioning. `v1.0.0` is the current stable release.
See [CHANGELOG.md](CHANGELOG.md) for release notes and
[CONTRIBUTING.md](CONTRIBUTING.md) for release details.

## Use

```go
package main

import (
	stdlog "log"

	"github.com/hitraa/gologger"
)

func main() {
	logFile := &gologger.FileConfig{
		Path:       "logs/service.log",
		MaxBytes:   10 << 20,
		MaxLines:   100_000,
		MaxBackups: 5,
		CreateDirs: true,
	}
	appLogger, err := gologger.New(gologger.Config{
		Level: gologger.LevelInfo,
		File:  logFile,
	})
	if err != nil {
		stdlog.Fatal(err)
	}
	defer appLogger.Close()

	if err := appLogger.Infof("service started on port %d", 8080); err != nil {
		stdlog.Println(err)
	}
}
```

The zero `Config.Level` defaults to INFO, and a nil `Config.Output` defaults to
`os.Stdout`, so terminal logging is enabled by default. File logging is
optional; when `File` is configured, records go to both destinations. Set
`Output` to `io.Discard` for file-only logging. Source locations are included
by default; set `DisableSource` to omit them. `ColorAuto` colors terminal
output by level (debug cyan, info green, warn yellow, error red); use `ColorNever`
to disable colors or `ColorAlways` to force them. Files always receive plain
text. Records use UTC timestamps with millisecond precision and the format
`[timestamp] (file.go:line) [LEVEL] message`. Newlines in messages are escaped
so each record occupies one physical line, and other control characters are
visibly escaped to prevent terminal control injection. `LevelOff` disables all
records.

## Examples

Run the basic example with `go run ./examples/basic`. It demonstrates level
parsing and changes, severity methods, colors, file rotation, sync, and close
error handling. Additional examples:

- `go run ./examples/structured`: JSON output and contextual/record fields.
- `go run ./examples/sampling`: sampling with rotation and age retention.
- `go run ./examples/slog`: `log/slog` integration (Go 1.21 or newer).

## Structured logging

Select `FormatJSON` for one JSON object per line, or use `With` and `LogFields`
to add attributes to text records:

```go
serviceLogger := appLogger.With(gologger.Field{Key: "service", Value: "vehicle-api"})
if err := serviceLogger.LogFields(
	gologger.LevelInfo,
	"vehicle connected",
	gologger.Field{Key: "vehicle_id", Value: 17},
); err != nil {
	stdlog.Println(err)
}
```

Field keys accept letters, digits, dots, hyphens, and underscores. JSON format
reserves `time`, `level`, `msg`, and `source` for record metadata.
See [`examples/structured`](examples/structured) for JSON output and
[`examples/slog`](examples/slog) for the Go 1.21+ adapter.

## Sampling

Sampling is disabled by default. Configure `SamplingConfig` to keep the first
`Initial` records per severity in each interval, then one in every
`Thereafter` records. ERROR records bypass sampling unless `SampleErrors` is
enabled. Sampling counters are bounded by severity, not by message cardinality.
See [`examples/sampling`](examples/sampling) for a complete sampling and
retention configuration.

## File rotation

`MaxBytes`, `MaxLines`, and `RotateInterval` are independent limits; reaching
any enabled limit rotates the active file before the next record. Zero disables
each criterion. `MaxAge` optionally deletes numbered backups older than the
configured duration. Opening an existing file counts its lines once, so line
rotation starts with the correct count; this scans the active file at startup.
Backups use numbered suffixes (`service.log.1`, `service.log.2`, ...), with
`.1` being the newest. A single record larger than `MaxBytes` is written to an
empty active file rather than repeatedly rotating. `MaxBackups: 0` truncates the
active file on rotation without retaining backups. `CreateDirs` creates missing
parent directories; the default file mode is `0640`.

## Lifecycle and errors

Logger methods return write errors; check them when output failures matter.
`Sync` flushes regular-file outputs, file logs, and custom writers that expose a
`Sync() error` method; it skips terminal and pipe `*os.File` outputs. `Close`
closes only the logger-owned file, not the caller-provided `Output` writer, and
is safe to call repeatedly. A logger serializes writes and configuration changes.
On Linux, macOS, and Windows, a file sink holds an exclusive non-blocking lock
on `Path.lock`; another logger opening that path receives `ErrFileInUse`. The
lock prevents concurrent rotation but does not merge multiple writers. Other
operating systems currently do not provide process-level file locking.

## `log/slog` integration

On Go 1.21 or newer, `Logger.SlogHandler()` adapts the logger to the standard
library `log/slog` API, including attributes, groups, source, and event time.
The core package continues to support Go 1.20; the adapter is excluded on that
version by a build constraint. `slog.Logger` methods don’t return output errors;
use the direct `Logger` methods when per-record write errors must be returned.
`Sync` and `Close` still report flush and shutdown errors.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
```

## License

Licensed under the Apache License, Version 2.0. Redistributions must include
the license and preserve the attribution in [NOTICE](NOTICE). See [LICENSE](LICENSE)
for the full terms.

## Maintainer

Created and maintained by Harshal Khairnar at Hitraa Technologies.

- Website: https://hitraa.com
- Contact: harshal@hitraa.com
