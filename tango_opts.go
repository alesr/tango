package tango

import (
	"log/slog"
	"time"
)

// Constants used in the system.
const (
	defaultAttemptToJoinFrequency  = time.Millisecond * 500
	defaultCheckDeadlinesFrequency = time.Second
	defaultTimeout                 = 5 * time.Second
	defaultPlayerQueueSize         = 100
)

type Option func(*Tango)

func WithLogger(logger *slog.Logger) Option {
	return func(t *Tango) {
		t.logger = logger
	}
}

func WithQueueSize(size int) Option {
	return func(t *Tango) {
		t.playerQueue = make(chan Player, size)
	}
}

func WithAttemptToJoinFrequency(frequency time.Duration) Option {
	return func(t *Tango) {
		t.attemptToJoinFrequency = frequency
	}
}

func WithCheckDeadlinesFrequency(frequency time.Duration) Option {
	return func(t *Tango) {
		t.checkDeadlinesFrequency = frequency
	}
}

func WithDefaultTimeout(timeout time.Duration) Option {
	return func(t *Tango) {
		t.defaultTimeout = timeout
	}
}
