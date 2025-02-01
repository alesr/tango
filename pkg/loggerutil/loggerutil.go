package loggerutil

import (
	"io"
	"log/slog"
)

func NoopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
