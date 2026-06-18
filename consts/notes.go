package consts

// Equal-temperament note frequencies in Hz. Naming follows scientific
// pitch notation: the number is the octave, letters use 'S' for sharp
// (e.g. FreqNoteCS4 is C#4). Flats are the enharmonic sharp of the
// lower note; use FreqNoteFS4 for Gb4 if it reads more naturally.
// The toolkit treats A4 = 440 Hz (ISO 16) as the reference; other
// tunings can be derived by multiplying by the tuning ratio.
const (
	// Octave 2 — sub-bass / low bass.
	FreqNoteC2  = 65.41
	FreqNoteCS2 = 69.30
	FreqNoteD2  = 73.42
	FreqNoteDS2 = 77.78
	FreqNoteE2  = 82.41
	FreqNoteF2  = 87.31
	FreqNoteFS2 = 92.50
	FreqNoteG2  = 98.00
	FreqNoteGS2 = 103.83
	FreqNoteA2  = 110.00
	FreqNoteAS2 = 116.54
	FreqNoteB2  = 123.47

	// Octave 3 — bass.
	FreqNoteC3  = 130.81
	FreqNoteCS3 = 138.59
	FreqNoteD3  = 146.83
	FreqNoteDS3 = 155.56
	FreqNoteE3  = 164.81
	FreqNoteF3  = 174.61
	FreqNoteFS3 = 185.00
	FreqNoteG3  = 196.00
	FreqNoteGS3 = 207.65
	FreqNoteA3  = 220.00
	FreqNoteAS3 = 233.08
	FreqNoteB3  = 246.94

	// Octave 4 — middle register (A4 = 440 Hz is the tuning reference).
	FreqNoteC4  = 261.63
	FreqNoteCS4 = 277.18
	FreqNoteD4  = 293.66
	FreqNoteDS4 = 311.13
	FreqNoteE4  = 329.63
	FreqNoteF4  = 349.23
	FreqNoteFS4 = 369.99
	FreqNoteG4  = 392.00
	FreqNoteGS4 = 415.30
	FreqNoteA4  = 440.00
	FreqNoteAS4 = 466.16
	FreqNoteB4  = 493.88

	// Octave 5 — upper middle.
	FreqNoteC5  = 523.25
	FreqNoteCS5 = 554.37
	FreqNoteD5  = 587.33
	FreqNoteDS5 = 622.25
	FreqNoteE5  = 659.25
	FreqNoteF5  = 698.46
	FreqNoteFS5 = 739.99
	FreqNoteG5  = 783.99
	FreqNoteGS5 = 830.61
	FreqNoteA5  = 880.00
	FreqNoteAS5 = 932.33
	FreqNoteB5  = 987.77

	// Octave 6 — treble.
	FreqNoteC6  = 1046.50
	FreqNoteCS6 = 1108.73
	FreqNoteD6  = 1174.66
	FreqNoteDS6 = 1244.51
	FreqNoteE6  = 1318.51
	FreqNoteF6  = 1396.91
	FreqNoteFS6 = 1479.98
	FreqNoteG6  = 1567.98
	FreqNoteGS6 = 1661.22
	FreqNoteA6  = 1760.00
	FreqNoteAS6 = 1864.66
	FreqNoteB6  = 1975.53

	// FreqNoteA is the ISO 16 tuning reference (A4 = 440 Hz). Alias
	// of FreqNoteA4 for concise code where the specific octave isn't
	// important.
	FreqNoteA = FreqNoteA4
)
