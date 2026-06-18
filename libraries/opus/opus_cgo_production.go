//go:build cgo && opus_production_c

package opus

/*
#cgo CFLAGS: -DOPUS_PRODUCTION_C
#cgo LDFLAGS: -lm
*/
import "C"
