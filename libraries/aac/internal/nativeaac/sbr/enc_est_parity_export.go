// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Parity-export shims for the SBR-encoder estimator batch (invf_est / nf_est):
// thin views the sbr-enc-est oracle uses. No logic — they expose package-private
// constants the test needs to size buffers identically to the C MAX_* macros.
package sbr

// MaxNumNoiseValues returns MAX_NUM_NOISE_VALUES (sbr_def.h), the cap on the
// per-frame INVF-mode / noise-level vectors.
func MaxNumNoiseValues() int { return encMaxNumNoiseValues }
