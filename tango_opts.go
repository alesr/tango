package tango

import (
	"log/slog"
	"sync"
	"time"
)

// Constants used in the system.
const (
	defaultAttemptToJoinFrequency  = time.Millisecond * 500
	defaultCheckDeadlinesFrequency = time.Second
	defaultTimeout                 = 5 * time.Second
	defaultOpBufferSize            = 100 // Buffer size for operation channels
	defaultMatchBufferSize         = 100 // Buffer size for match channels
	defaultNumWorkers              = 10
	defaultJobBufferSize           = 1000 // TODO: Investigate a strategy to infer a suitable buffer size
)

// Options defines the function for applying optional configuration to the Tango instance.
type Option func(*Tango)

// WithLogger sets the logger for Tango.
func WithLogger(logger *slog.Logger) Option {
	return func(t *Tango) {
		t.logger = logger
	}
}

// WithOperationBufferSize sets the buffer size for operation channels.
func WithOperationBufferSize(size int) Option {
	return func(t *Tango) {
		if size <= 0 {
			size = defaultOpBufferSize
		}
		t.opCh = make(chan operation, size)
		t.timeoutCh = make(chan string, size)
	}
}

// WithMatchBufferSize sets the buffer size for match channels.
func WithMatchBufferSize(size int) Option {
	return func(t *Tango) {
		if size <= 0 {
			size = defaultMatchBufferSize
		}
		// Ensure matches are created with proper buffer sizes
		t.matchPool = sync.Pool{
			New: func() any {
				return &match{
					joinedPlayers: sync.Map{},
					requestCh:     make(chan matchRequest, size),
					doneCh:        make(chan struct{}),
				}
			},
		}
	}
}

// WithAttemptToJoinFrequency sets the frequency for matching attempts.
func WithAttemptToJoinFrequency(frequency time.Duration) Option {
	return func(t *Tango) {
		if frequency <= 0 {
			frequency = defaultAttemptToJoinFrequency
		}
		t.attemptToJoinFrequency = frequency
	}
}

// WithCheckDeadlinesFrequency sets the frequency for checking player deadlines.
func WithCheckDeadlinesFrequency(frequency time.Duration) Option {
	return func(t *Tango) {
		if frequency <= 0 {
			frequency = defaultCheckDeadlinesFrequency
		}
		t.checkDeadlinesFrequency = frequency
	}
}

// WithDefaultTimeout sets the default operation timeout.
func WithDefaultTimeout(timeout time.Duration) Option {
	return func(t *Tango) {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		t.defaultTimeout = timeout
	}
}

// WithWorkerPool sets the configuration for the matchmaking worker pool.
func WithWorkerPool(numWorkers, jobBufferSize int) Option {
	return func(t *Tango) {
		if numWorkers <= 0 {
			numWorkers = defaultNumWorkers
		}
		if jobBufferSize <= 0 {
			jobBufferSize = defaultJobBufferSize
		}
		t.matchWorkers = newMatchWorkerPool(numWorkers, jobBufferSize, t)
	}
}

// WithStatsUpdateInterval sets how frequently stats are updated
func WithStatsUpdateInterval(interval time.Duration) Option {
	return func(t *Tango) {
		if interval > 0 {
			t.statsUpdateInterval = interval
		}
	}
}
