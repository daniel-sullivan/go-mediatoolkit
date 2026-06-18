//go:build windows

package devices

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// TestGUIDLiterals verifies the WASAPI GUID constants against the
// canonical values published by Microsoft in mmdeviceapi.h.
func TestGUIDLiterals(t *testing.T) {
	cases := []struct {
		name string
		want windows.GUID
		got  windows.GUID
	}{
		{
			name: "CLSID_MMDeviceEnumerator",
			want: windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}},
			got:  clsidMMDeviceEnumerator,
		},
		{
			name: "IID_IMMDeviceEnumerator",
			want: windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}},
			got:  iidIMMDeviceEnumerator,
		},
		{
			name: "IID_IMMNotificationClient",
			want: windows.GUID{Data1: 0x7991EEC9, Data2: 0x7E89, Data3: 0x4D85, Data4: [8]byte{0x83, 0x90, 0x6C, 0x70, 0x3C, 0xEC, 0x60, 0xC0}},
			got:  iidIMMNotificationClient,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

// TestPropertyKeys verifies the PKEY fmtid GUIDs and pids.
func TestPropertyKeys(t *testing.T) {
	cases := []struct {
		name     string
		wantGUID windows.GUID
		wantPID  uint32
		got      propertyKey
	}{
		{
			name:     "PKEY_Device_FriendlyName",
			wantGUID: windows.GUID{Data1: 0xA45C254E, Data2: 0xDF1C, Data3: 0x4EFD, Data4: [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0}},
			wantPID:  14,
			got:      pkeyDeviceFriendlyName,
		},
		{
			name:     "PKEY_AudioEngine_DeviceFormat",
			wantGUID: windows.GUID{Data1: 0xF19F064D, Data2: 0x082C, Data3: 0x4E27, Data4: [8]byte{0xBC, 0x73, 0x68, 0x82, 0xA1, 0xBB, 0x8E, 0x4C}},
			wantPID:  0,
			got:      pkeyAudioEngineDeviceFormat,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantGUID, tc.got.fmtid)
			assert.Equal(t, tc.wantPID, tc.got.pid)
		})
	}
}

// TestParseWaveformatex covers the pure-Go WAVEFORMATEX byte-layout
// reader used by describeDevice to extract sample rate and channel
// count from PKEY_AudioEngine_DeviceFormat BLOBs.
func TestParseWaveformatex(t *testing.T) {
	cases := []struct {
		name         string
		build        func() []byte
		wantChannels int
		wantRate     int
		wantOK       bool
	}{
		{
			name: "stereo 48kHz 16-bit",
			build: func() []byte {
				b := make([]byte, 18)
				binary.LittleEndian.PutUint16(b[0:], 1)      // WAVE_FORMAT_PCM
				binary.LittleEndian.PutUint16(b[2:], 2)      // nChannels
				binary.LittleEndian.PutUint32(b[4:], 48000)  // nSamplesPerSec
				binary.LittleEndian.PutUint32(b[8:], 192000) // nAvgBytesPerSec
				binary.LittleEndian.PutUint16(b[12:], 4)     // nBlockAlign
				binary.LittleEndian.PutUint16(b[14:], 16)    // wBitsPerSample
				binary.LittleEndian.PutUint16(b[16:], 0)     // cbSize
				return b
			},
			wantChannels: 2,
			wantRate:     48000,
			wantOK:       true,
		},
		{
			name: "mono 44.1kHz",
			build: func() []byte {
				b := make([]byte, 18)
				binary.LittleEndian.PutUint16(b[0:], 1)
				binary.LittleEndian.PutUint16(b[2:], 1)
				binary.LittleEndian.PutUint32(b[4:], 44100)
				return b
			},
			wantChannels: 1,
			wantRate:     44100,
			wantOK:       true,
		},
		{
			name: "too short",
			build: func() []byte {
				return make([]byte, 10)
			},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ch, sr, ok := parseWaveformatex(tc.build())
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantChannels, ch)
				assert.Equal(t, tc.wantRate, sr)
			}
		})
	}
}

// TestPropVariantLPWSTR builds a synthetic PROPVARIANT holding a
// VT_LPWSTR payload and asserts the extractor returns the original
// string.
func TestPropVariantLPWSTR(t *testing.T) {
	// Prepare a UTF-16 null-terminated buffer on the Go heap and keep
	// it alive for the duration of the test.
	buf, err := windows.UTF16FromString("Speakers (Realtek)")
	require.NoError(t, err)

	pv := propVariant{vt: vtLPWSTR}
	pv.val0 = uintptr(unsafe.Pointer(&buf[0]))

	assert.Equal(t, uint16(vtLPWSTR), pv.vt)
	assert.Equal(t, "Speakers (Realtek)", pv.lpwstr())
}

// TestPropVariantBlob validates the BLOB payload view used for
// WAVEFORMATEX delivery.
func TestPropVariantBlob(t *testing.T) {
	payload := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	pv := propVariant{vt: vtBlob}
	// BLOB: cbSize (uint32) in val0, pBlobData (pointer) in val1.
	pv.val0 = uintptr(len(payload))
	pv.val1 = uintptr(unsafe.Pointer(&payload[0]))

	got := pv.blob()
	require.Len(t, got, len(payload))
	assert.Equal(t, payload, got)
}

// TestPropVariantUI4 validates the VT_UI4 extractor.
func TestPropVariantUI4(t *testing.T) {
	pv := propVariant{vt: vtUI4}
	pv.val0 = 0xCAFEBABE
	assert.Equal(t, uint32(0xCAFEBABE), pv.ui4())
}

// TestHRESULTError checks that S_OK maps to nil and a non-zero HRESULT
// renders a readable hex string.
func TestHRESULTError(t *testing.T) {
	assert.NoError(t, errFromHRESULT("op", 0))

	err := errFromHRESULT("IMMDevice::GetId", 0x80070005) // E_ACCESSDENIED
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IMMDevice::GetId")
	assert.Contains(t, err.Error(), "0x80070005")

	hre, ok := err.(*hresultError)
	require.True(t, ok)
	assert.Equal(t, hresult(0x80070005), hre.Code())
}
