// Package tvh contains the Tvheadend HTTP API client and its supporting
// tolerant JSON decoders.
package tvh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// FlexInt decodes JSON numbers, numeric strings, bools and null into an int64.
// Tvheadend is inconsistent about JSON types, so every numeric field uses it.
//
// Integers are parsed exactly via strconv.ParseInt (equivalent to routing
// through json.Number) so values above 2^53 do not lose precision. Only
// non-integer forms (e.g. "7.0") fall back to float parsing.
type FlexInt int64

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case bytes.Equal(b, []byte("null")), bytes.Equal(b, []byte(`""`)):
		*f = 0
		return nil
	case bytes.Equal(b, []byte("true")):
		*f = 1
		return nil
	case bytes.Equal(b, []byte("false")):
		*f = 0
		return nil
	case len(b) > 0 && b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		n, err := parseFlexInt(s)
		if err != nil {
			return fmt.Errorf("FlexInt: %q is not numeric", s)
		}
		*f = FlexInt(n)
		return nil
	default:
		n, err := parseFlexInt(string(b))
		if err != nil {
			return err
		}
		*f = FlexInt(n)
		return nil
	}
}

// parseFlexInt parses s into an int64, trying an exact integer parse first
// (strconv.ParseInt) and only falling back to float parsing for non-integer
// forms such as "7.0". This avoids the precision loss that routing every
// value through float64 would cause above 2^53.
func parseFlexInt(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	fl, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(fl), nil
}

// Int64 returns the value.
func (f FlexInt) Int64() int64 { return int64(f) }

// FlexBool decodes bools, 0/1 and "true"/"false"/"0"/"1".
type FlexBool bool

func (f *FlexBool) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		b = []byte(strings.ToLower(strings.TrimSpace(s)))
		if len(b) == 0 {
			*f = false
			return nil
		}
	}
	switch string(b) {
	case "true", "1":
		*f = true
	case "false", "0", "null":
		*f = false
	default:
		var n float64
		if err := json.Unmarshal(b, &n); err != nil {
			return fmt.Errorf("FlexBool: cannot decode %s", b)
		}
		*f = n != 0
	}
	return nil
}

// Bool returns the value.
func (f FlexBool) Bool() bool { return bool(f) }

// FlexString decodes strings, numbers, bools and null into a string.
type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*f = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = FlexString(s)
		return nil
	}
	*f = FlexString(b)
	return nil
}

// String returns the value.
func (f FlexString) String() string { return string(f) }
