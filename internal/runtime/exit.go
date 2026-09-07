package runtime

// Exit is the erased result of interpreting one effect program. Its zero value
// is an unsuccessful termination with the empty cause, which keeps the typed
// public wrapper's zero value meaningful.
type Exit struct {
	cause     Cause
	value     any
	succeeded bool
}

// Success constructs a successful erased exit.
func Success(value any) Exit {
	return Exit{value: value, succeeded: true}
}

// Failure constructs an unsuccessful erased exit.
func Failure(cause Cause) Exit {
	return Exit{cause: cause}
}

// Succeeded reports whether interpretation produced a value.
func (x Exit) Succeeded() bool {
	return x.succeeded
}

// Value returns the erased success value. Its dynamic type is the A channel of
// the effect that produced it.
func (x Exit) Value() any {
	return x.value
}

// Cause returns the complete termination cause.
func (x Exit) Cause() Cause {
	return x.cause
}
