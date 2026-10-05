package gologger

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type rotatingFile struct {
	path       string
	maxBytes   int64
	maxLines   int64
	maxBackups int
	mode       os.FileMode
	file       *os.File
	bytes      int64
	lines      int64
}

func openRotatingFile(config FileConfig) (*rotatingFile, error) {
	if config.Path == "" {
		return nil, errors.New("logger: file path must not be empty")
	}
	if config.MaxBytes < 0 || config.MaxLines < 0 || config.MaxBackups < 0 {
		return nil, errors.New("logger: rotation limits must not be negative")
	}
	if config.CreateDirs {
		if err := os.MkdirAll(filepath.Dir(config.Path), 0750); err != nil {
			return nil, fmt.Errorf("logger: create log directory: %w", err)
		}
	}
	mode := config.FileMode
	if mode == 0 {
		mode = 0640
	}

	file := &rotatingFile{
		path:       config.Path,
		maxBytes:   config.MaxBytes,
		maxLines:   config.MaxLines,
		maxBackups: config.MaxBackups,
		mode:       mode,
	}
	if err := file.openActive(); err != nil {
		return nil, err
	}
	return file, nil
}

func (file *rotatingFile) openActive() error {
	active, err := os.OpenFile(file.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, file.mode)
	if err != nil {
		return fmt.Errorf("open %q: %w", file.path, err)
	}
	info, err := active.Stat()
	if err != nil {
		_ = active.Close()
		return fmt.Errorf("stat %q: %w", file.path, err)
	}
	lines, err := countLines(file.path)
	if err != nil {
		_ = active.Close()
		return fmt.Errorf("count lines in %q: %w", file.path, err)
	}
	file.file = active
	file.bytes = info.Size()
	file.lines = lines
	return nil
}

func countLines(path string) (int64, error) {
	input, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer input.Close()

	reader := bufio.NewReader(input)
	buffer := make([]byte, 32*1024)
	var lines int64
	var size int64
	var last byte
	for {
		count, readErr := reader.Read(buffer)
		for _, value := range buffer[:count] {
			if value == '\n' {
				lines++
			}
			last = value
		}
		size += int64(count)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, readErr
		}
	}
	if size > 0 && last != '\n' {
		lines++
	}
	return lines, nil
}

func (file *rotatingFile) WriteRecord(record []byte) error {
	if file.file == nil {
		return errors.New("file sink is unavailable")
	}
	recordLines := int64(0)
	for _, value := range record {
		if value == '\n' {
			recordLines++
		}
	}
	if recordLines == 0 {
		recordLines = 1
	}

	rotateForBytes := file.maxBytes > 0 && file.bytes > 0 && int64(len(record)) > file.maxBytes-file.bytes
	rotateForLines := file.maxLines > 0 && file.lines > 0 && file.lines+recordLines > file.maxLines
	if rotateForBytes || rotateForLines {
		if err := file.rotate(); err != nil {
			return err
		}
	}

	written, err := file.file.Write(record)
	file.bytes += int64(written)
	if written > 0 {
		for _, value := range record[:written] {
			if value == '\n' {
				file.lines++
			}
		}
	}
	if err != nil {
		return err
	}
	if written != len(record) {
		return io.ErrShortWrite
	}
	return nil
}

func (file *rotatingFile) rotate() error {
	if err := file.file.Close(); err != nil {
		file.file = nil
		return file.reopenAfterRotationError(fmt.Errorf("close before rotation: %w", err))
	}
	file.file = nil

	if file.maxBackups == 0 {
		active, err := os.OpenFile(file.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, file.mode)
		if err != nil {
			return fmt.Errorf("truncate %q: %w", file.path, err)
		}
		file.file = active
		file.bytes = 0
		file.lines = 0
		return nil
	}
	oldestPath := fmt.Sprintf("%s.%d", file.path, file.maxBackups)
	if err := os.Remove(oldestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return file.reopenAfterRotationError(err)
	}

	for backup := file.maxBackups - 1; backup >= 1; backup-- {
		oldPath := fmt.Sprintf("%s.%d", file.path, backup)
		newPath := fmt.Sprintf("%s.%d", file.path, backup+1)
		if _, err := os.Stat(oldPath); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return file.reopenAfterRotationError(err)
		}
		if err := os.Remove(newPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return file.reopenAfterRotationError(err)
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return file.reopenAfterRotationError(err)
		}
	}
	if err := os.Rename(file.path, file.path+".1"); err != nil {
		return file.reopenAfterRotationError(err)
	}
	if err := file.openActive(); err != nil {
		return fmt.Errorf("create active log after rotation: %w", err)
	}
	return nil
}

func (file *rotatingFile) reopenAfterRotationError(cause error) error {
	if err := file.openActive(); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (file *rotatingFile) Sync() error {
	if file.file == nil {
		return errors.New("file sink is unavailable")
	}
	return file.file.Sync()
}

func (file *rotatingFile) Close() error {
	if file.file == nil {
		return nil
	}
	return file.file.Close()
}
