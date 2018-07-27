package common

import (
	"fmt"
)

// A panic resulting from a sanity check means there is a programmer error
// and some guarantee is not satisfied.
func PanicSanity(v interface{}) {
	panic(Fmt("Panicked on a Sanity Check: %v", v))
}

// Like fmt.Sprintf, but skips formatting if args are empty.
var Fmt = func(format string, a ...interface{}) string {
	if len(a) == 0 {
		return format
	}
	return fmt.Sprintf(format, a...)
}

// Indicates a failure of consensus. Someone was malicious or something has
// gone horribly wrong. These should really boot us into an "emergency-recover" mode
// XXX DEPRECATED
func PanicConsensus(v interface{}) {
	panic(Fmt("Panicked on a Consensus Failure: %v", v))
}
