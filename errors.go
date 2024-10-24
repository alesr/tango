package tango

// TangoError represents a base error type for the package.
type TangoError struct {
	title string
}

// Error returns the string representation to a TangoErroor.
func (e TangoError) Error() string {
	return e.title
}

func newTangoError(title string) TangoError {
	return TangoError{title: title}
}

// Enumerate package errors.
type (
	InvalidQueueSizeError      struct{ TangoError }
	PlayerAlreadyEnqueuedError struct{ TangoError }
	ServiceAlreadyStartedError struct{ TangoError }
	ServiceNotStartedError     struct{ TangoError }
	PlayerNotFoundError        struct{ TangoError }
)

// Enumerate errors for internal use.
var (
	errInvalidQueueSize      = InvalidQueueSizeError{newTangoError("queue size must be greater than 0")}
	errPlayerAlreadyEnqueued = PlayerAlreadyEnqueuedError{newTangoError("player already enqueued")}
	errServiceAlreadyStarted = ServiceAlreadyStartedError{newTangoError("tango service already started")}
	errServiceNotStarted     = ServiceNotStartedError{newTangoError("tango service not started")}
	errPlayerNotFound        = PlayerNotFoundError{newTangoError("player not found")}
)
