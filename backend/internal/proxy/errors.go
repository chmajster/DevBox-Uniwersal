package proxy

import "errors"

var (
	ErrNotFound     = errors.New("networking resource not found")
	ErrConflict     = errors.New("networking resource conflict")
	ErrInvalidInput = errors.New("invalid networking input")
	ErrPortInUse    = errors.New("port is already reserved or in use")
	ErrNoPorts      = errors.New("no free ports available")
)

type PrivilegeError struct {
	Operation   string
	Path        string
	Instruction string
	Err         error
}

func (e *PrivilegeError) Error() string {
	if e.Path == "" {
		return e.Operation + " requires additional privileges"
	}
	return e.Operation + " requires write access to " + e.Path
}

func (e *PrivilegeError) Unwrap() error { return e.Err }
