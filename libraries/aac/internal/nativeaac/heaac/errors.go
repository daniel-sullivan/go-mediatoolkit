// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package heaac

import "errors"

// errUnsupportedConfig is returned when the HE-AAC v1 element/QMF configuration
// cannot be set up (e.g. an out-of-range sample rate or a config the legacy SBR
// path does not support).
var errUnsupportedConfig = errors.New("aac: unsupported HE-AAC configuration")
