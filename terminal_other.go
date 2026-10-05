//go:build !linux && !darwin && !windows

package gologger

import "os"

func isTerminal(*os.File) bool { return false }
