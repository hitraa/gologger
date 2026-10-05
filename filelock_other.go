//go:build !linux && !darwin && !windows

package gologger

import "os"

func lockFileExclusive(*os.File) error { return nil }

func unlockFile(*os.File) error { return nil }
