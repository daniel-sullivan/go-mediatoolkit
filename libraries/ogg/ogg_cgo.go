//go:build cgo

package ogg

/*
#cgo CFLAGS: -DHAVE_CONFIG_H
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/libogg
#cgo CFLAGS: -I${SRCDIR}/libogg/include
#cgo CFLAGS: -O2
#cgo CFLAGS: -Wno-unused-parameter

#include <ogg/ogg.h>
*/
import "C"
