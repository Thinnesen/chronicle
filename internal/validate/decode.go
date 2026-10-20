package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
)

// badDuration finds the value in an error of time.ParseDuration.
var badDuration = regexp.MustCompile(`duration "(.*)"$`)

// durationFields are the JSON names of the duration fields of the API.
var durationFields = []string{"timeout", "heartbeatTimeout", "scheduleToStartTimeout", "duration", "maxDuration", "maxAge", "deleteDelayDuration"}

// Strict decodes JSON into v. An unknown field is an error. The Go decoder gives no field path for a bad duration,
// so Strict finds the fields that have the bad value and names them.
func Strict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		return nil
	}
	m := badDuration.FindStringSubmatch(err.Error())
	var doc any
	if m == nil || json.Unmarshal(data, &doc) != nil {
		return err
	}
	paths := find(doc, m[1], "", "")
	if len(paths) == 0 {
		return err
	}
	slices.Sort(paths)
	return fmt.Errorf("%s: %w", paths[0], err)
}

// find returns the paths of the duration fields in a JSON document whose value is equal to value.
func find(doc any, value, path, key string) []string {
	var out []string
	switch d := doc.(type) {
	case string:
		if d == value && slices.Contains(durationFields, key) {
			out = append(out, path)
		}
	case map[string]any:
		for k, c := range d {
			p := k
			if path != "" {
				p = path + "." + k
			}
			out = append(out, find(c, value, p, k)...)
		}
	case []any:
		for i, c := range d {
			out = append(out, find(c, value, path+"["+strconv.Itoa(i)+"]", "")...)
		}
	}
	return out
}
