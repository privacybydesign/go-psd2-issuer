package logging

import "testing"

func TestMaskIdentifier(t *testing.T) {
	cases := map[string]string{
		"":                                     "",
		"short":                                "*****",
		"NL91ABNA0417164300":                   "NL91**********4300",
		"af86536d-34e3-4ebc-6c64-08db56ccef25": "af86****************************ef25",
	}
	for in, want := range cases {
		if got := MaskIdentifier(in); got != want {
			t.Errorf("MaskIdentifier(%q) = %q, want %q", in, got, want)
		}
	}
}
