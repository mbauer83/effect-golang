package unit

// A shared vocabulary of failure, and adopting a library's fault into it.

import (
	"context"
	"errors"
	"fmt"
	goruntime "runtime"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/effect/fault"
)

// storeFault stands for a library's own fault, which states its kind.
type storeFault struct{ key string }

func (storeFault) Error() string    { return "store: get: not held" }
func (storeFault) Kind() fault.Kind { return fault.Missing }

func TestAnAdoptedFaultKeepsTheKindItsLibraryStated(t *testing.T) {
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	lookup := effect.Fail[effect.Unit, string](storeFault{key: "a"}).MapError(fault.From)

	cause, failed := runtime.Run(context.Background(), effect.Unit{}, lookup).Cause()
	why, found := cause.Failure()
	if !failed || !found {
		t.Fatal("expected the lookup to fail")
	}
	if why.Kind() != fault.Missing {
		t.Errorf("expected missing, got %s", why.Kind())
	}
	var original storeFault
	if !errors.As(why, &original) || original.key != "a" {
		t.Errorf("expected the library's fault to stay reachable, got %v", why)
	}
}

func TestAKindSurvivesWrapping(t *testing.T) {
	wrapped := fmt.Errorf("reading the cart: %w", fault.Unacceptable.About("the cart", errors.New("it is closed")))
	if fault.KindOf(wrapped) != fault.Unacceptable {
		t.Errorf("expected unacceptable through the wrapping, got %s", fault.KindOf(wrapped))
	}
	if fault.KindOf(errors.New("connection reset")) != fault.Unavailable {
		t.Error("expected an unclassified error to read as unavailable")
	}
}

func TestAFaultReadsAsKindSubjectAndCause(t *testing.T) {
	why := fault.Missing.About("the film", errors.New("nobody has heard of it"))
	if why.Error() != "missing: the film: nobody has heard of it" || why.Message() != "nobody has heard of it" {
		t.Errorf("unexpected rendering %q / %q", why.Error(), why.Message())
	}
	if fault.From(storeFault{}).Error() != "missing: store: get: not held" {
		t.Errorf("unexpected rendering without a subject: %q", fault.From(storeFault{}).Error())
	}
}

func TestATaskFailureSaysWhichLineRaisedIt(t *testing.T) {
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	_, file, line, _ := goruntime.Caller(0)
	refusing := effect.FromFault[int](fault.Unacceptable.About("the order", errors.New("it is paid")))
	program := effect.FromValue(1).FlatMap(func(int) effect.Task[int] { return refusing })

	cause, failed := runtime.Run(context.Background(), effect.Unit{}, program).Cause()
	if !failed {
		t.Fatal("expected the failure")
	}
	if where := fmt.Sprintf("%s:%d", file, line+1); cause.Origin().Source != where {
		t.Errorf("expected %s, got %q", where, cause.Origin().Source)
	}
	exit := runtime.Run(context.Background(), effect.Unit{}, effect.Done())
	if _, succeeded := exit.Value(); !succeeded {
		t.Error("expected Done to succeed")
	}
}

func TestAdoptingAFaultKeepsItAsItIs(t *testing.T) {
	why := fault.Missing.About("the film", errors.New("nobody has heard of it"))
	if adopted := fault.From(error(why)); adopted != why {
		t.Errorf("expected the fault unchanged, got %q", adopted.Error())
	}
}
