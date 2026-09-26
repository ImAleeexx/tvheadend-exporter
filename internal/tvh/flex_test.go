package tvh

import (
	"encoding/json"
	"testing"
)

func TestFlexInt(t *testing.T) {
	cases := map[string]int64{`5`: 5, `"6"`: 6, `true`: 1, `false`: 0, `null`: 0, `7.0`: 7, `""`: 0}
	for in, want := range cases {
		var v FlexInt
		if err := json.Unmarshal([]byte(in), &v); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if v.Int64() != want {
			t.Errorf("%s: want %d got %d", in, want, v)
		}
	}
	var v FlexInt
	if err := json.Unmarshal([]byte(`"abc"`), &v); err == nil {
		t.Error("want error for non-numeric string")
	}
}

func TestFlexInt_StringNumber(t *testing.T) {
	var s struct {
		Errors FlexInt `json:"errors"`
	}
	if err := json.Unmarshal([]byte(`{"errors":"6"}`), &s); err != nil || s.Errors != 6 {
		t.Fatalf("got %v %v", s.Errors, err)
	}
}

// TestFlexInt_LargeValueNoPrecisionLoss guards against decoding integers
// through float64, which silently loses precision above 2^53 (9007199254740992).
// See preflight D11/N11: integers must be parsed exactly via strconv.ParseInt /
// json.Number, falling back to float parsing only for non-integer forms.
func TestFlexInt_LargeValueNoPrecisionLoss(t *testing.T) {
	const big int64 = 9007199254740993 // 2^53 + 1, unrepresentable exactly as float64
	cases := []string{
		`9007199254740993`,
		`"9007199254740993"`,
	}
	for _, in := range cases {
		var v FlexInt
		if err := json.Unmarshal([]byte(in), &v); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if v.Int64() != big {
			t.Errorf("%s: want %d got %d", in, big, v.Int64())
		}
	}
}

func TestFlexBool(t *testing.T) {
	cases := map[string]bool{`true`: true, `false`: false, `1`: true, `0`: false, `"true"`: true, `"false"`: false, `"1"`: true, `null`: false}
	for in, want := range cases {
		var v FlexBool
		if err := json.Unmarshal([]byte(in), &v); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if v.Bool() != want {
			t.Errorf("%s: want %v got %v", in, want, v)
		}
	}
}

func TestFlexString(t *testing.T) {
	cases := map[string]string{`"a"`: "a", `5`: "5", `true`: "true", `null`: ""}
	for in, want := range cases {
		var v FlexString
		if err := json.Unmarshal([]byte(in), &v); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if v.String() != want {
			t.Errorf("%s: want %q got %q", in, want, v)
		}
	}
}
