package projects

import "errors"

var ErrInvalidInput = errors.New("invalid directory input")

type Service struct{ directoryBrowseRoots []string }
