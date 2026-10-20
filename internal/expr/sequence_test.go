package expr

import (
	"math"
	"slices"
	"testing"
)

func TestSequence(t *testing.T) {
	tests := []struct {
		start, end int64
		format     string
		want       []string
		err        bool
	}{
		{0, 2, "", []string{"0", "1", "2"}, false},
		{9, 10, "day-%02d", []string{"day-09", "day-10"}, false},
		{10, 11, "user%02X", []string{"user0A", "user0B"}, false},
		{5, 4, "", []string{}, false}, // count 0
		{0, 1, "%s", nil, true},
		{0, 1, "%d-%d", nil, true},
		{0, 1000, "", nil, true},                      // more than 1000 items
		{math.MinInt64, math.MaxInt64, "", nil, true}, // end-start overflows
		{math.MaxInt64 - 1, math.MaxInt64, "", []string{"9223372036854775806", "9223372036854775807"}, false},
	}
	for _, tt := range tests {
		got, err := Sequence(tt.start, tt.end, tt.format)
		if (err != nil) != tt.err || !tt.err && !slices.Equal(got, tt.want) {
			t.Errorf("Sequence(%d, %d, %q) = %q, %v", tt.start, tt.end, tt.format, got, err)
		}
	}
}
