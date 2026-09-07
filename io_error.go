package effect

import "fmt"

// IOOperation names one base filesystem operation.
type IOOperation string

const (
	IOReadFile  IOOperation = "read-file"
	IOWriteFile IOOperation = "write-file"
	IOStat      IOOperation = "stat"
	IOReadDir   IOOperation = "read-dir"
	IOMkdirAll  IOOperation = "mkdir-all"
	IORemove    IOOperation = "remove"
	IORename    IOOperation = "rename"
)

// IOError preserves a filesystem operation's structured context and wrapped
// platform error.
type IOError struct {
	Operation  IOOperation
	Path       string
	TargetPath string
	Err        error
}

func (failure IOError) Error() string {
	if failure.TargetPath == "" {
		return fmt.Sprintf("%s %q: %v", failure.Operation, failure.Path, failure.Err)
	}
	return fmt.Sprintf(
		"%s %q -> %q: %v",
		failure.Operation,
		failure.Path,
		failure.TargetPath,
		failure.Err,
	)
}

// Unwrap exposes the platform error for errors.Is and errors.As.
func (failure IOError) Unwrap() error {
	return failure.Err
}
