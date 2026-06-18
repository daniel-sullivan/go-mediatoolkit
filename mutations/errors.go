package mutations

import "errors"

// ErrUnsortedEnvelope is returned by ValidateGainEnvelope when points
// are not in non-decreasing time order.
var ErrUnsortedEnvelope = errors.New("mutations: envelope points must be in non-decreasing time order")
