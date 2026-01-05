package main

import (
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// logger wraps zerolog for structured logging.
type logger struct {
	z zerolog.Logger
}

// newLogger creates a logger with console output.
func newLogger() *logger {
	noColor := os.Getenv("NO_COLOR") != ""
	if fi, err := os.Stderr.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
		noColor = true
	}

	out := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
		NoColor:    noColor,
	}
	zl := zerolog.New(out).With().Timestamp().Logger()
	return &logger{z: zl}
}

func (l *logger) info(msg string) { l.z.Info().Msg(msg) }
func (l *logger) warn(msg string) { l.z.Warn().Msg(msg) }
func (l *logger) ok(msg string)   { l.z.Info().Msg(msg) }
func (l *logger) err(msg string)  { l.z.Error().Msg(msg) }

func (l *logger) infof(format string, args ...any) { l.info(fmt.Sprintf(format, args...)) }
func (l *logger) warnf(format string, args ...any) { l.warn(fmt.Sprintf(format, args...)) }
func (l *logger) okf(format string, args ...any)   { l.ok(fmt.Sprintf(format, args...)) }
