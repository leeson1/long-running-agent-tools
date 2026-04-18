package task

import "errors"

// ErrTaskCancelled indicates the task was cancelled by the user.
var ErrTaskCancelled = errors.New("task cancelled")
