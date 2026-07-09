package aec3

// clamp32 is rtc::SafeClamp<float>'s non-pointer equivalent (NaN
// clamps to lo), used by the ERL/ERLE estimators, reverb estimators,
// and suppression filter (Phase 4/5) to clamp computed values rather
// than in-place config fields. Kept here, independent of
// github.com/daniel-sullivan/go-mediatoolkit/aec/config's
// Config/Validate — it's a generic numeric helper, not part of the
// Config type tree that package owns.
func clamp32(v, lo, hi float32) float32 {
	if !(v >= lo) { // also catches NaN, matching rtc::SafeClamp
		return lo
	} else if v > hi {
		return hi
	}
	return v
}
