package aec

import (
	"fmt"
	"reflect"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
)

// validateTuning checks a caller-supplied CancellerConfig.Tuning
// against config.Validate's range rules. config.Validate only clamps
// out-of-range fields in place (matching upstream's own tolerant
// EchoCanceller3Config::Validate) rather than rejecting them; this
// wrapper runs Validate on a copy and, if it had to change anything,
// treats that as an invalid Tuning value: a caller that never wanted
// FirstHFBand silently pinned to a different number, say, gets
// ErrBadArg instead — naming the exact field and its clamped value —
// rather than the input being silently replaced out from under them.
func validateTuning(c *config.Config) error {
	validated := *c
	config.Validate(&validated)
	if reflect.DeepEqual(*c, validated) {
		return nil
	}
	path, before, after := diffFirstField(reflect.ValueOf(*c), reflect.ValueOf(validated), "")
	return fmt.Errorf("%w: Tuning%s out of range (%v clamped to %v)", ErrBadArg, path, before, after)
}

// diffFirstField walks two identically-typed struct values field by
// field (recursing into nested structs) and returns the dotted path
// to the first leaf field that differs, plus its two values. This is
// a generic reflect-based walk rather than a hand-maintained
// field-by-field comparison, so validateTuning's error detail stays
// accurate as config.Config's field set evolves, with no separate
// list to keep in sync.
func diffFirstField(orig, validated reflect.Value, path string) (fieldPath string, before, after any) {
	t := orig.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported; config.Config has none, but be defensive
			continue
		}
		ov := orig.Field(i)
		vv := validated.Field(i)
		fp := path + "." + f.Name
		if ov.Kind() == reflect.Struct {
			if sub, b, a := diffFirstField(ov, vv, fp); sub != "" {
				return sub, b, a
			}
			continue
		}
		if ov.Interface() != vv.Interface() {
			return fp, ov.Interface(), vv.Interface()
		}
	}
	return "", nil, nil
}
