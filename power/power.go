// Package power prevents automatic idle system sleep while monitoring.
// It does not keep the display on or override lid closure or explicit sleep.
package power

// Acquire requests idle sleep prevention. The returned release function is safe
// to call repeatedly. Failure is non-fatal: callers may continue monitoring.
func Acquire() (release func(), err error) {
	return acquire()
}
