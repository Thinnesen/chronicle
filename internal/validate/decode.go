package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// badValue finds the bad value in an error of time.ParseDuration or of a time in RFC 3339.
var badValue = regexp.MustCompile(`(?:duration|parsing time) "(.*?)"`)

// durationFields are the JSON names of the duration fields of the API. timeFields are those of the times.
var (
	durationFields = []string{"timeout", "heartbeatTimeout", "scheduleToStartTimeout", "duration", "maxDuration", "maxAge", "deleteDelayDuration"}
	timeFields     = []string{"startAt"}
)

// Strict decodes JSON into v. An unknown field is an error. The Go decoder gives no field path for a bad duration or
// time, so Strict finds the fields that have the bad value and names them. It also refuses a negative duration, as the
// CRD does.
func Strict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	var doc any
	if json.Unmarshal(data, &doc) != nil {
		return err
	}
	if err == nil {
		if paths := find(doc, func(key, value string) bool {
			return strings.HasPrefix(value, "-") && slices.Contains(durationFields, key)
		}, ""); len(paths) > 0 {
			slices.Sort(paths)
			return fmt.Errorf("%s: a duration cannot be negative", paths[0])
		}
		return nil
	}
	m := badValue.FindStringSubmatch(err.Error())
	if m == nil {
		return err
	}
	paths := find(doc, func(key, value string) bool {
		return value == m[1] && (slices.Contains(durationFields, key) || slices.Contains(timeFields, key))
	}, "")
	if len(paths) == 0 {
		return err
	}
	slices.Sort(paths)
	return fmt.Errorf("%s: %w", paths[0], err)
}

// find returns the paths of the string values in a JSON document that match, by their key and value.
func find(doc any, match func(key, value string) bool, path string) []string {
	var out []string
	switch d := doc.(type) {
	case map[string]any:
		for k, c := range d {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if v, ok := c.(string); ok && match(k, v) {
				out = append(out, p)
			}
			out = append(out, find(c, match, p)...)
		}
	case []any:
		for i, c := range d {
			out = append(out, find(c, match, path+"["+strconv.Itoa(i)+"]")...)
		}
	}
	return out
}
