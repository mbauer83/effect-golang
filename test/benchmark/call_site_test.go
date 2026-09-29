package benchmark

import (
	"errors"
	"testing"

	"github.com/mbauer83/effect-golang/effect"
)

var refused = errors.New("refused")

// Describing a failure records the line that raised it; on a refusal path
// that is once per request.
func BenchmarkDescribingAFailure(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = effect.Fail[effect.Unit, int](refused)
	}
}

func BenchmarkDescribingASpan(b *testing.B) {
	b.ReportAllocs()
	work := effect.Succeed[effect.Unit, error](1)
	for b.Loop() {
		_ = work.WithSpan("verify the access token")
	}
}
