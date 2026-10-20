package expr

import (
	"fmt"
	"regexp"
)

// MaxItems is the largest fan-out.
const MaxItems = 1000

// sequenceFormat is a format with exactly one integer verb, as Argo gives it to Sprintf.
var sequenceFormat = regexp.MustCompile(`^([^%]|%%)*%[-+ #0]*[0-9]*[dxXob]([^%]|%%)*$`)

// CheckSequenceFormat returns an error when a withSequence format does not have exactly one integer verb.
func CheckSequenceFormat(format string) error {
	if format != "" && !sequenceFormat.MatchString(format) {
		return fmt.Errorf("format must have one number verb, such as %%02d or %%x: %q", format)
	}
	return nil
}

// Sequence returns the items of a withSequence from start to end, both included. An end before start gives no items.
func Sequence(start, end int64, format string) ([]string, error) {
	if err := CheckSequenceFormat(format); err != nil {
		return nil, err
	}
	if format == "" {
		format = "%d"
	}
	if end-start >= MaxItems {
		return nil, fmt.Errorf("the sequence has more than %d items", MaxItems)
	}
	items := []string{}
	for i := start; i <= end; i++ {
		items = append(items, fmt.Sprintf(format, i))
	}
	return items, nil
}
