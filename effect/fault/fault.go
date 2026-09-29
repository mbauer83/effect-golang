// Package fault is a vocabulary of failure that every layer of a program can
// share: which kind of thing went wrong, what it was about, and the error
// behind it.
//
// Four kinds, because four decisions are made from them. A caller retries
// what was Unavailable, responds to Missing with "not found", reports Unreadable to
// whoever maintains the thing it read, and passes Unacceptable on to the
// caller who asked. Every fault the effect-golang modules fail with states its
// kind, so an application translates a library's fault into its own with
// From, and chooses a status once, from the kind.
package fault

import "errors"

// Kind is what sort of failure a fault is: the part a caller decides on.
type Kind uint8

const (
	// Unavailable is a thing that is there and could not be reached. Asking
	// again later may help. It is the kind of an error that states none.
	Unavailable Kind = iota
	// Missing is a thing that is not there. Asking again will not help.
	Missing
	// Unreadable is a response or document that could not be understood, or a
	// program that could not state what it meant. Somebody has to look.
	Unreadable
	// Unacceptable is a legitimate no: a request that breaks a rule, a
	// credential that does not check out. Which rule is the wrapped error.
	Unacceptable
)

func (kind Kind) String() string {
	switch kind {
	case Missing:
		return "missing"
	case Unreadable:
		return "unreadable"
	case Unacceptable:
		return "unacceptable"
	default:
		return "unavailable"
	}
}

// Classified is an error that states its kind. Every fault type of the
// effect-golang modules is one.
type Classified interface {
	error
	Kind() Kind
}

// KindOf is the kind the first Classified error in err's chain states, and
// Unavailable when none does: an error nobody classified is most often a
// connection that failed, and retrying it later is the safe reading.
func KindOf(err error) Kind {
	var classified Classified
	if errors.As(err, &classified) {
		return classified.Kind()
	}
	return Unavailable
}

// Fault is why an operation failed.
type Fault struct {
	kind    Kind
	subject string
	err     error
}

// About is a fault of this kind about a subject, caused by err.
//
//	fault.Missing.About("the session", redis.ErrNil)
func (kind Kind) About(subject string, err error) Fault {
	return Fault{kind: kind, subject: subject, err: err}
}

// From is err as a fault of the kind it states, and a fault as it is. It is
// what MapError is given to adopt a library's fault:
//
//	redis.Connect[effect.Unit](scope, options).MapError(fault.From)
func From[E error](err E) Fault {
	var adopted error = err
	if already, isFault := adopted.(Fault); isFault {
		return already
	}
	return Fault{kind: KindOf(err), err: err}
}

func (fault Fault) Kind() Kind      { return fault.kind }
func (fault Fault) Subject() string { return fault.subject }
func (fault Fault) Unwrap() error   { return fault.err }

// Message is the cause's own sentence, without the kind and the subject.
func (fault Fault) Message() string {
	if fault.err == nil {
		return fault.kind.String()
	}
	return fault.err.Error()
}

func (fault Fault) Error() string {
	if fault.subject == "" {
		return fault.kind.String() + ": " + fault.Message()
	}
	return fault.kind.String() + ": " + fault.subject + ": " + fault.Message()
}
