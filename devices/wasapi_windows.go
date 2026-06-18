//go:build windows

package devices

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WASAPI / MMDevice API COM bindings used by the Windows backend.
//
// GUIDs are verified against Microsoft's SDK headers:
//
//   - mmdeviceapi.h                      (MMDevice interfaces, DEVICE_STATE_* flags)
//   - functiondiscoverykeys_devpkey.h    (PKEY_Device_FriendlyName)
//   - mmreg.h                            (WAVEFORMATEX, WAVE_FORMAT_PCM)
//   - propkeydef.h                       (PROPERTYKEY layout)
//
// Every method on every vtable runs through syscall.SyscallN; callers
// hold a pointer to the COM object and index into vtbl. The layout of
// each Vtbl struct mirrors the C interface declarations one-to-one
// including the three IUnknown slots at the top.

// GUIDs — all values verified against Microsoft SDK headers.
var (
	clsidMMDeviceEnumerator  = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator   = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIMMNotificationClient = windows.GUID{Data1: 0x7991EEC9, Data2: 0x7E89, Data3: 0x4D85, Data4: [8]byte{0x83, 0x90, 0x6C, 0x70, 0x3C, 0xEC, 0x60, 0xC0}}

	// IID_IAudioClient {1CB9AD4C-DBFA-4C32-B178-C2F568A703B2}
	iidIAudioClient = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	// IID_IAudioRenderClient {F294ACFC-3146-4483-A7BF-ADDCA7C260E2}
	iidIAudioRenderClient = windows.GUID{Data1: 0xF294ACFC, Data2: 0x3146, Data3: 0x4483, Data4: [8]byte{0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2}}
	// IID_IAudioCaptureClient {C8ADBD64-E71E-48A0-A4DE-185C395CD317}
	iidIAudioCaptureClient = windows.GUID{Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0, Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}

	// KSDATAFORMAT_SUBTYPE_IEEE_FLOAT {00000003-0000-0010-8000-00AA00389B71}
	ksDataFormatSubtypeIEEEFloat = windows.GUID{Data1: 0x00000003, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	// KSDATAFORMAT_SUBTYPE_PCM {00000001-0000-0010-8000-00AA00389B71}
	ksDataFormatSubtypePCM = windows.GUID{Data1: 0x00000001, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
)

// AUDCLNT_* flags and share modes.
const (
	audclntShareModeShared = 0

	audclntStreamflagsEventCallback     uint32 = 0x00040000
	audclntStreamflagsAutoconvertPCM    uint32 = 0x80000000
	audclntStreamflagsSRCDefaultQuality uint32 = 0x08000000
	audclntStreamflagsNoPersist         uint32 = 0x00080000

	waveFormatExtensible uint16 = 0xFFFE
	waveFormatPCM        uint16 = 0x0001
	waveFormatIEEEFloat  uint16 = 0x0003

	clsctxAll uint32 = 0x17

	audclntEBufferSizeNotAligned = 0x88890019
)

// EDataFlow values for EnumAudioEndpoints / GetDefaultAudioEndpoint.
const (
	eRender  = 0
	eCapture = 1
	eAll     = 2
)

// ERole values for GetDefaultAudioEndpoint.
const (
	eConsole        = 0
	eMultimedia     = 1
	eCommunications = 2
)

// Device-state bits for EnumAudioEndpoints.
const (
	deviceStateActive     = 0x00000001
	deviceStateDisabled   = 0x00000002
	deviceStateNotPresent = 0x00000004
	deviceStateUnplugged  = 0x00000008
	deviceStateMaskAll    = 0x0000000F
)

// STGM flags for OpenPropertyStore.
const (
	stgmRead = 0x00000000
)

// VARTYPE codes that the backend cares about. PROPVARIANT uses the
// same set as VARIANT.
const (
	vtEmpty   = 0
	vtI4      = 3
	vtUI4     = 19
	vtLPWSTR  = 31
	vtBlob    = 65
	vtClsid   = 72
	vtArray   = 0x2000
	vtByref   = 0x4000
	vtVariant = 12
)

// propertyKey mirrors the C PROPERTYKEY struct: { GUID fmtid, DWORD pid }.
type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

// PKEY values — fmtid values are stable UUIDs defined in the headers
// cited at the top of the file.
var (
	// PKEY_Device_FriendlyName {A45C254E-DF1C-4EFD-8020-67D146A850E0}, 14
	pkeyDeviceFriendlyName = propertyKey{
		fmtid: windows.GUID{Data1: 0xA45C254E, Data2: 0xDF1C, Data3: 0x4EFD, Data4: [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0}},
		pid:   14,
	}
	// PKEY_AudioEngine_DeviceFormat {F19F064D-082C-4E27-BC73-6882A1BB8E4C}, 0
	pkeyAudioEngineDeviceFormat = propertyKey{
		fmtid: windows.GUID{Data1: 0xF19F064D, Data2: 0x082C, Data3: 0x4E27, Data4: [8]byte{0xBC, 0x73, 0x68, 0x82, 0xA1, 0xBB, 0x8E, 0x4C}},
		pid:   0,
	}
)

// propVariant is the Go view of the Windows PROPVARIANT structure. The
// Windows definition is a tagged union; we only need to interpret a
// handful of variants (LPWSTR, UI4, BLOB). The structure is
// intentionally defined large enough to cover the biggest payload we
// read — Windows guarantees sizeof(PROPVARIANT) == 16 on 32-bit and 24
// on 64-bit systems, and any pointer union member lives at offset 8.
//
// Layout:
//
//	offset 0:   VARTYPE vt        (uint16)
//	offset 2:   WORD    wReserved1
//	offset 4:   WORD    wReserved2
//	offset 6:   WORD    wReserved3
//	offset 8:   union   { pwszVal, blob, ulVal, ... }
//
// On 64-bit platforms the union is 16 bytes (pointer + 8 bytes of
// padding for BLOB.cbSize/pBlobData). We encode the union as two
// uintptrs which is large enough on both 386 and amd64/arm64.
type propVariant struct {
	vt         uint16
	wReserved1 uint16
	wReserved2 uint16
	wReserved3 uint16
	// Union: lay out as two uintptrs + two uint32s so we can access
	// BLOB.cbSize (uint32) followed by BLOB.pBlobData (pointer) as well
	// as the single-pointer variants like LPWSTR.
	val0 uintptr
	val1 uintptr
}

// lpwstr returns the PROPVARIANT's value as a Go string, assuming it
// holds a VT_LPWSTR. Callers must have checked vt.
func (p *propVariant) lpwstr() string {
	if p.val0 == 0 {
		return ""
	}
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(p.val0)))
}

// blob returns the PROPVARIANT's BLOB payload as a byte slice view into
// Windows-owned memory. Callers must not retain the slice past the
// matching propVariantClear.
func (p *propVariant) blob() []byte {
	// BLOB { DWORD cbSize; BYTE *pBlobData; } starts at offset 8. With
	// our uintptr layout val0 == cbSize (low 32 bits) and val1 ==
	// pBlobData.
	size := uint32(p.val0)
	if size == 0 || p.val1 == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(p.val1)), int(size))
}

// ui4 returns the PROPVARIANT's VT_UI4 value.
func (p *propVariant) ui4() uint32 { return uint32(p.val0) }

// propVariantClear calls ole32!PropVariantClear on a PROPVARIANT so
// Windows can free any heap memory it owns.
var procPropVariantClear = ole32.NewProc("PropVariantClear")

func propVariantClear(pv *propVariant) {
	procPropVariantClear.Call(uintptr(unsafe.Pointer(pv)))
}

// waveformatex mirrors the first 18 bytes of a WAVEFORMATEX blob.
//
//	typedef struct tWAVEFORMATEX {
//	  WORD  wFormatTag;
//	  WORD  nChannels;
//	  DWORD nSamplesPerSec;
//	  DWORD nAvgBytesPerSec;
//	  WORD  nBlockAlign;
//	  WORD  wBitsPerSample;
//	  WORD  cbSize;
//	} WAVEFORMATEX;
type waveformatex struct {
	wFormatTag      uint16
	nChannels       uint16
	nSamplesPerSec  uint32
	nAvgBytesPerSec uint32
	nBlockAlign     uint16
	wBitsPerSample  uint16
	cbSize          uint16
}

// parseWaveformatex reads a WAVEFORMATEX out of a raw byte slice, which
// is how PKEY_AudioEngine_DeviceFormat delivers the value. Returns
// (channels, sample rate, ok).
func parseWaveformatex(b []byte) (channels int, sampleRate int, ok bool) {
	if len(b) < 18 {
		return 0, 0, false
	}
	wf := (*waveformatex)(unsafe.Pointer(&b[0]))
	return int(wf.nChannels), int(wf.nSamplesPerSec), true
}

// -- IMMDeviceEnumerator ----------------------------------------------

type iMMDeviceEnumerator struct {
	vtbl *iMMDeviceEnumeratorVtbl
}

type iMMDeviceEnumeratorVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr

	EnumAudioEndpoints                     uintptr
	GetDefaultAudioEndpoint                uintptr
	GetDevice                              uintptr
	RegisterEndpointNotificationCallback   uintptr
	UnregisterEndpointNotificationCallback uintptr
}

// EnumAudioEndpoints filters by dataflow (eRender/eCapture) and by
// device-state mask and returns an IMMDeviceCollection.
func (e *iMMDeviceEnumerator) EnumAudioEndpoints(dataflow uint32, stateMask uint32) (*iMMDeviceCollection, error) {
	var coll *iMMDeviceCollection
	r, _, _ := syscall.SyscallN(e.vtbl.EnumAudioEndpoints,
		uintptr(unsafe.Pointer(e)), uintptr(dataflow), uintptr(stateMask), uintptr(unsafe.Pointer(&coll)))
	if err := errFromHRESULT("IMMDeviceEnumerator::EnumAudioEndpoints", r); err != nil {
		return nil, err
	}
	return coll, nil
}

// GetDefaultAudioEndpoint returns the current default device for the
// given dataflow and role.
func (e *iMMDeviceEnumerator) GetDefaultAudioEndpoint(dataflow uint32, role uint32) (*iMMDevice, error) {
	var dev *iMMDevice
	r, _, _ := syscall.SyscallN(e.vtbl.GetDefaultAudioEndpoint,
		uintptr(unsafe.Pointer(e)), uintptr(dataflow), uintptr(role), uintptr(unsafe.Pointer(&dev)))
	if err := errFromHRESULT("IMMDeviceEnumerator::GetDefaultAudioEndpoint", r); err != nil {
		return nil, err
	}
	return dev, nil
}

// GetDevice fetches an IMMDevice by endpoint ID string. Endpoint IDs
// are the same LPWSTRs returned by IMMDevice::GetId.
func (e *iMMDeviceEnumerator) GetDevice(id string) (*iMMDevice, error) {
	wid, err := syscall.UTF16PtrFromString(id)
	if err != nil {
		return nil, err
	}
	var dev *iMMDevice
	r, _, _ := syscall.SyscallN(e.vtbl.GetDevice,
		uintptr(unsafe.Pointer(e)),
		uintptr(unsafe.Pointer(wid)),
		uintptr(unsafe.Pointer(&dev)))
	if err := errFromHRESULT("IMMDeviceEnumerator::GetDevice", r); err != nil {
		return nil, err
	}
	return dev, nil
}

// RegisterEndpointNotificationCallback registers a notification sink.
// The pointer passed must be an IMMNotificationClient* — typically the
// address of a notificationClient value.
func (e *iMMDeviceEnumerator) RegisterEndpointNotificationCallback(client unsafe.Pointer) error {
	r, _, _ := syscall.SyscallN(e.vtbl.RegisterEndpointNotificationCallback,
		uintptr(unsafe.Pointer(e)), uintptr(client))
	return errFromHRESULT("IMMDeviceEnumerator::RegisterEndpointNotificationCallback", r)
}

// UnregisterEndpointNotificationCallback removes a previously registered sink.
func (e *iMMDeviceEnumerator) UnregisterEndpointNotificationCallback(client unsafe.Pointer) error {
	r, _, _ := syscall.SyscallN(e.vtbl.UnregisterEndpointNotificationCallback,
		uintptr(unsafe.Pointer(e)), uintptr(client))
	return errFromHRESULT("IMMDeviceEnumerator::UnregisterEndpointNotificationCallback", r)
}

// Release drops the reference count.
func (e *iMMDeviceEnumerator) Release() uint32 {
	r, _, _ := syscall.SyscallN(e.vtbl.Release, uintptr(unsafe.Pointer(e)))
	return uint32(r)
}

// -- IMMDeviceCollection ----------------------------------------------

type iMMDeviceCollection struct {
	vtbl *iMMDeviceCollectionVtbl
}

type iMMDeviceCollectionVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr

	GetCount uintptr
	Item     uintptr
}

// GetCount returns the number of devices in the collection.
func (c *iMMDeviceCollection) GetCount() (uint32, error) {
	var n uint32
	r, _, _ := syscall.SyscallN(c.vtbl.GetCount, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(&n)))
	if err := errFromHRESULT("IMMDeviceCollection::GetCount", r); err != nil {
		return 0, err
	}
	return n, nil
}

// Item fetches the i'th device.
func (c *iMMDeviceCollection) Item(i uint32) (*iMMDevice, error) {
	var dev *iMMDevice
	r, _, _ := syscall.SyscallN(c.vtbl.Item,
		uintptr(unsafe.Pointer(c)), uintptr(i), uintptr(unsafe.Pointer(&dev)))
	if err := errFromHRESULT("IMMDeviceCollection::Item", r); err != nil {
		return nil, err
	}
	return dev, nil
}

// Release drops the reference count.
func (c *iMMDeviceCollection) Release() uint32 {
	r, _, _ := syscall.SyscallN(c.vtbl.Release, uintptr(unsafe.Pointer(c)))
	return uint32(r)
}

// -- IMMDevice --------------------------------------------------------

type iMMDevice struct {
	vtbl *iMMDeviceVtbl
}

type iMMDeviceVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr

	Activate          uintptr
	OpenPropertyStore uintptr
	GetId             uintptr
	GetState          uintptr
}

// OpenPropertyStore opens the device's property bag.
func (d *iMMDevice) OpenPropertyStore(stgm uint32) (*iPropertyStore, error) {
	var store *iPropertyStore
	r, _, _ := syscall.SyscallN(d.vtbl.OpenPropertyStore,
		uintptr(unsafe.Pointer(d)), uintptr(stgm), uintptr(unsafe.Pointer(&store)))
	if err := errFromHRESULT("IMMDevice::OpenPropertyStore", r); err != nil {
		return nil, err
	}
	return store, nil
}

// Activate instantiates an interface on the device — used to obtain an
// IAudioClient via iid IID_IAudioClient.
func (d *iMMDevice) Activate(iid *windows.GUID, clsctx uint32) (unsafe.Pointer, error) {
	var ppv unsafe.Pointer
	r, _, _ := syscall.SyscallN(d.vtbl.Activate,
		uintptr(unsafe.Pointer(d)),
		uintptr(unsafe.Pointer(iid)),
		uintptr(clsctx),
		0, // activationParams — unused for audio interfaces
		uintptr(unsafe.Pointer(&ppv)))
	if err := errFromHRESULT("IMMDevice::Activate", r); err != nil {
		return nil, err
	}
	return ppv, nil
}

// GetId returns the device endpoint ID as a Go string. Windows
// allocates the underlying LPWSTR; this call frees it before
// returning.
func (d *iMMDevice) GetId() (string, error) {
	var id *uint16
	r, _, _ := syscall.SyscallN(d.vtbl.GetId,
		uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&id)))
	if err := errFromHRESULT("IMMDevice::GetId", r); err != nil {
		return "", err
	}
	s := windows.UTF16PtrToString(id)
	windows.CoTaskMemFree(unsafe.Pointer(id))
	return s, nil
}

// GetState returns the DEVICE_STATE_* flags.
func (d *iMMDevice) GetState() (uint32, error) {
	var state uint32
	r, _, _ := syscall.SyscallN(d.vtbl.GetState,
		uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&state)))
	if err := errFromHRESULT("IMMDevice::GetState", r); err != nil {
		return 0, err
	}
	return state, nil
}

// Release drops the reference count.
func (d *iMMDevice) Release() uint32 {
	r, _, _ := syscall.SyscallN(d.vtbl.Release, uintptr(unsafe.Pointer(d)))
	return uint32(r)
}

// -- IPropertyStore ---------------------------------------------------

type iPropertyStore struct {
	vtbl *iPropertyStoreVtbl
}

type iPropertyStoreVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr

	GetCount uintptr
	GetAt    uintptr
	GetValue uintptr
	SetValue uintptr
	Commit   uintptr
}

// GetValue reads a PROPVARIANT for the given key. The caller is
// responsible for calling propVariantClear when done.
func (s *iPropertyStore) GetValue(key *propertyKey, pv *propVariant) error {
	r, _, _ := syscall.SyscallN(s.vtbl.GetValue,
		uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(pv)))
	return errFromHRESULT("IPropertyStore::GetValue", r)
}

// Release drops the reference count.
func (s *iPropertyStore) Release() uint32 {
	r, _, _ := syscall.SyscallN(s.vtbl.Release, uintptr(unsafe.Pointer(s)))
	return uint32(r)
}
