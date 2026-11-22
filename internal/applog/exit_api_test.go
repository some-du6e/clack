package applog

import "path/filepath"

// The exit tests' reads of the run record (a red run replaces this file
// with the old API's: one record per log directory).

func startRun(l *Logger, instance, header string) string { return l.StartRun(instance, header) }

func runFile(dir, instance string) string { return filepath.Join(dir, RunFile(instance)) }
