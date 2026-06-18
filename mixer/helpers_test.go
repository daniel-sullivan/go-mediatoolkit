package mixer

import "io"

// ioEOF is a shared sentinel for test helpers; aliases io.EOF so the
// test package avoids duplicate imports.
var ioEOF = io.EOF
