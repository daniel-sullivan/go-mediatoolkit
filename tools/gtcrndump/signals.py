"""
Shared deterministic signal set + JSON helpers for the GTCRN golden generators.

Interpreter: /Users/daniel/ml-venv/bin/python3

The canonical signals are produced with a self-contained splitmix64 PRNG and
closed-form math (NO numpy.random) so the EXACT float32 samples are byte-for-byte
reproducible in a future Go port. Every generator imports these definitions so
the four deterministic signals are identical across all three golden files.

Signals (16 kHz, mono, float32):
  1. white-noise   amp 0.1, seed 0x5EED0001, 3 s (144000... -> 48000 samples)
  2. sine-sweep    100->3500 Hz linear chirp, amp 0.5, 3 s (closed-form phase)
  3. impulse-train click every 1200 samples, amp 0.8, 2 s
  4. silence       1 s of zeros
"""
import math
import numpy as np

FS = 16000

# --- splitmix64 -------------------------------------------------------------
_MASK64 = (1 << 64) - 1


def splitmix64_stream(seed: int, n: int) -> np.ndarray:
    """Return n uint64 draws from splitmix64 seeded with `seed`.

    Reference algorithm (Vigna). State starts at `seed`; each draw advances the
    state by the golden-ratio increment 0x9E3779B97F4A7C15, then applies the
    finalizing mix. Reproducible bit-for-bit in Go with uint64 arithmetic.
    """
    out = np.empty(n, dtype=np.uint64)
    state = seed & _MASK64
    for i in range(n):
        state = (state + 0x9E3779B97F4A7C15) & _MASK64
        z = state
        z = ((z ^ (z >> 30)) * 0xBF58476D1CE4E5B9) & _MASK64
        z = ((z ^ (z >> 27)) * 0x94D049BB133111EB) & _MASK64
        z = z ^ (z >> 31)
        out[i] = z
    return out


def _u64_to_unit_double(u: np.ndarray) -> np.ndarray:
    """uint64 -> float64 in [0,1) using the top 53 bits (standard construction)."""
    return (u >> 11).astype(np.float64) * (2.0 ** -53)


# --- canonical signals ------------------------------------------------------

def sig_white_noise() -> np.ndarray:
    n = 3 * FS
    u = splitmix64_stream(0x5EED0001, n)
    d = _u64_to_unit_double(u)          # [0,1)
    x = (2.0 * d - 1.0) * 0.1           # [-0.1, 0.1)
    return x.astype(np.float32)


def sig_sine_sweep() -> np.ndarray:
    n = 3 * FS
    f0, f1, amp = 100.0, 3500.0, 0.5
    T = n / FS
    k = (f1 - f0) / T                    # Hz per second
    idx = np.arange(n, dtype=np.float64)
    t = idx / FS
    # closed-form instantaneous phase of a linear chirp (float64), sample -> float32 once
    phase = 2.0 * math.pi * (f0 * t + 0.5 * k * t * t)
    x = amp * np.sin(phase)
    return x.astype(np.float32)


def sig_impulse_train() -> np.ndarray:
    n = 2 * FS
    x = np.zeros(n, dtype=np.float32)
    x[::1200] = np.float32(0.8)
    return x


def sig_silence() -> np.ndarray:
    return np.zeros(FS, dtype=np.float32)


def canonical_signals() -> dict:
    """name -> (samples float32, meta dict) for the 4 regenerable signals."""
    return {
        "white-noise": (sig_white_noise(), {
            "generator": "splitmix64", "seed": "0x5EED0001",
            "amplitude": 0.1, "duration_s": 3, "n": 3 * FS,
            "map": "u01=(draw>>11)*2^-53; x=(2*u01-1)*0.1"}),
        "sine-sweep": (sig_sine_sweep(), {
            "generator": "closed-form-linear-chirp",
            "f0_hz": 100.0, "f1_hz": 3500.0, "amplitude": 0.5,
            "duration_s": 3, "n": 3 * FS,
            "phase": "2*pi*(f0*t + 0.5*((f1-f0)/T)*t^2), t=idx/fs, T=n/fs"}),
        "impulse-train": (sig_impulse_train(), {
            "generator": "impulse-train", "period_samples": 1200,
            "amplitude": 0.8, "duration_s": 2, "n": 2 * FS,
            "note": "click at n%1200==0 (including n=0)"}),
        "silence": (sig_silence(), {
            "generator": "zeros", "duration_s": 1, "n": FS}),
    }


# --- JSON helpers -----------------------------------------------------------

def to_list(a) -> list:
    """numpy array -> nested python lists with full float32/float value precision.

    np.ndarray.tolist() yields python floats carrying the exact value of the
    stored float32 (widened to double), which json.dumps then emits at full
    round-trippable repr.
    """
    return np.asarray(a).tolist()


def tensor_field(a) -> dict:
    a = np.asarray(a)
    return {"shape": list(a.shape), "data": a.tolist()}
