package containers

import "errors"

var (
	ErrBadFormat    = errors.New("containers: malformed container")
	ErrUnsupported  = errors.New("containers: unsupported feature")
	ErrNeedSeeker   = errors.New("containers: writer requires io.WriteSeeker")
	ErrHeaderLocked = errors.New("containers: header cannot be changed after first write")
)
