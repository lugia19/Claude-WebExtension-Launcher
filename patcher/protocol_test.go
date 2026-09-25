package patcher

import "testing"

func TestPatchProtocolArray(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		patched bool
	}{
		{
			name:    "double-quoted (Windows/macOS)",
			in:      `x=new Set(["devtools:","file:","app:"]);`,
			want:    `x=new Set(["devtools:","file:","app:","chrome-extension:"]);`,
			patched: true,
		},
		{
			name:    "backtick-quoted (Linux)",
			in:      "x=new Set([`devtools:`,`file:`,`app:`]);",
			want:    "x=new Set([`devtools:`,`file:`,`app:`,`chrome-extension:`]);",
			patched: true,
		},
		{
			name:    "already present",
			in:      "x=new Set([`devtools:`,`file:`,`chrome-extension:`]);",
			want:    "x=new Set([`devtools:`,`file:`,`chrome-extension:`]);",
			patched: false,
		},
		{
			name:    "no array in chunk",
			in:      `something entirely different`,
			want:    `something entirely different`,
			patched: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, patched := patchProtocolArray([]byte(c.in))
			if patched != c.patched {
				t.Fatalf("patched = %v, want %v", patched, c.patched)
			}
			if string(got) != c.want {
				t.Fatalf("got %s\nwant %s", got, c.want)
			}
		})
	}
}
