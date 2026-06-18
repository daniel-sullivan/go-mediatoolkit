//go:build windows

package devices

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// COM helpers and HRESULT handling for the WASAPI backend.
//
// All WASAPI calls are plain vtable dispatches through syscall.SyscallN;
// no CGo. HRESULTs are coerced through hresultError which preserves the
// raw code so callers can pattern-match if needed.

// hresult is a 32-bit Windows HRESULT returned by COM methods.
type hresult uint32

// hresultError adapts an HRESULT to the error interface and keeps the
// raw code for callers that want to compare against specific failure
// constants.
type hresultError struct {
	op   string
	code hresult
}

// Error renders the HRESULT in hex alongside the failing operation name.
func (e *hresultError) Error() string {
	return fmt.Sprintf("devices: %s failed: HRESULT(0x%08x)", e.op, uint32(e.code))
}

// Code returns the raw HRESULT value.
func (e *hresultError) Code() hresult { return e.code }

// errFromHRESULT returns nil for S_OK and a *hresultError otherwise.
func errFromHRESULT(op string, r uintptr) error {
	code := hresult(r)
	if code == hresult(windows.S_OK) {
		return nil
	}
	return &hresultError{op: op, code: code}
}

// comInitState tracks whether the caller initialised COM on this
// goroutine and therefore owns the corresponding CoUninitialize.
type comInitState int

const (
	comInitNotOwned comInitState = iota
	comInitOwned
)

// coInitialize calls CoInitializeEx(MULTITHREADED). S_FALSE — returned
// when COM is already initialised on this goroutine — is treated as a
// no-op; the caller in that case does not own the uninitialise.
//
// RPC_E_CHANGED_MODE (0x80010106) means another call on this goroutine
// already picked a different apartment; the caller should proceed
// without owning CoUninitialize.
func coInitialize() (comInitState, error) {
	const rpcEChangedMode = 0x80010106
	err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	if err == nil {
		return comInitOwned, nil
	}
	errno, ok := err.(syscall.Errno)
	if !ok {
		return comInitNotOwned, err
	}
	switch uint32(errno) {
	case uint32(windows.S_FALSE):
		return comInitNotOwned, nil
	case rpcEChangedMode:
		return comInitNotOwned, nil
	}
	return comInitNotOwned, err
}

// coUninitialize releases the apartment if coInitialize claimed one.
func coUninitialize(state comInitState) {
	if state == comInitOwned {
		windows.CoUninitialize()
	}
}

// ole32 is the lazy-loaded shim for CoCreateInstance. The rest of the
// COM surface we need (CoInitializeEx, CoUninitialize, CoTaskMemFree)
// is exposed directly by golang.org/x/sys/windows.
var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

// coCreateInstance wraps ole32!CoCreateInstance.
//
//	HRESULT CoCreateInstance(
//	  REFCLSID  rclsid,
//	  LPUNKNOWN pUnkOuter,
//	  DWORD     dwClsContext,
//	  REFIID    riid,
//	  LPVOID    *ppv);
func coCreateInstance(clsid *windows.GUID, clsctx uint32, iid *windows.GUID) (unsafe.Pointer, error) {
	var ppv unsafe.Pointer
	r, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(clsid)),
		0,
		uintptr(clsctx),
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&ppv)),
	)
	if err := errFromHRESULT("CoCreateInstance", r); err != nil {
		return nil, err
	}
	return ppv, nil
}

// CLSCTX values accepted by CoCreateInstance.
const (
	clsctxInprocServer = 0x1
)
