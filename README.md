# gologger

A small Go logging library with no external dependencies. It writes serialized,
UTC-stamped records to an `io.Writer`, an optional rotating log file, or both.

## Install

```sh
go get github.com/hitraa/gologger@v1.0.0
```

Requires Go 1.20 or newer.

gologger follows Semantic Versioning. The exported `gologger.Version` value,
changelog entry, and release tag are kept in sync. See
[CHANGELOG.md](CHANGELOG.md) for release history and
[CONTRIBUTING.md](CONTRIBUTING.md) for release steps.

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
so each record occupies one physical line. `LevelOff` disables all records.

## File rotation

`MaxBytes` and `MaxLines` are independent limits; reaching either rotates the
active file before the next record. Non-positive limits disable that criterion.
Backups use numbered suffixes (`service.log.1`, `service.log.2`, ...), with
`.1` being the newest. A single record larger than `MaxBytes` is written to an
empty active file rather than repeatedly rotating. `MaxBackups: 0` truncates the
active file on rotation without retaining backups. `CreateDirs` creates missing
parent directories; the default file mode is `0640`.

## Lifecycle and errors

Logger methods return write errors; check them when output failures matter.
`Sync` flushes destinations that expose a `Sync() error` method. `Close` closes
only the logger-owned file, not the caller-provided `Output` writer, and is
safe to call repeatedly. A logger serializes writes and configuration changes;
separate processes should not write to the same rotating file concurrently.

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
