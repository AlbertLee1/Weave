package testprofile

// Instrumented reports whether Go test instrumentation that distorts
// wall-clock performance gates is active.
func Instrumented(coverMode string) bool {
	return RaceDetector || coverMode != ""
}
