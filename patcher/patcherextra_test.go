package patcher

import (
	"strings"
	"testing"
)

// TestPatchProtocolArrayDoubleQuote covers the long-standing generic anchor used
// by the Windows/macOS bundles: ["devtools:","file:".
func TestPatchProtocolArrayDoubleQuote(t *testing.T) {
	input := []byte(`const dDe = ["devtools:","file:"];`)
	out, ok := patchProtocolArray(input)
	if !ok {
		t.Fatalf("expected patch to apply for double-quoted array")
	}
	want := `const dDe = ["devtools:","file:","chrome-extension:"];`
	if string(out) != want {
		t.Fatalf("unexpected result:\n got: %s\nwant: %s", out, want)
	}
}

// TestPatchProtocolArrayBacktick covers the Linux bundle, which quotes protocol
// literals with template-literal backticks: [`devtools:`,`file:`.
func TestPatchProtocolArrayBacktick(t *testing.T) {
	// Mirror the real Linux chunk declaration, including the extra app: entry
	// and a trailing comma before the closing bracket.
	input := []byte("const dDe=[`devtools:`,`file:`,`app:`];")
	out, ok := patchProtocolArray(input)
	if !ok {
		t.Fatalf("expected patch to apply for backtick-quoted array")
	}
	want := "const dDe=[`devtools:`,`file:`,`app:`,`chrome-extension:`];"
	if string(out) != want {
		t.Fatalf("unexpected result:\n got: %s\nwant: %s", out, want)
	}
}

// TestPatchProtocolArrayIdempotent confirms the patch is a no-op once the entry
// is already present, for both quote styles.
func TestPatchProtocolArrayIdempotent(t *testing.T) {
	cases := []string{
		`const dDe = ["devtools:","file:","chrome-extension:"];`,
		"const dDe=[`devtools:`,`file:`,`app:`,`chrome-extension:`];",
	}
	for _, in := range cases {
		_, ok := patchProtocolArray([]byte(in))
		if ok {
			t.Fatalf("expected no-op (idempotent) for: %s", in)
		}
	}
}

// TestPatchProtocolArrayNoMatch ensures an unrelated chunk is untouched.
func TestPatchProtocolArrayNoMatch(t *testing.T) {
	input := []byte(`const foo = 42;`)
	out, ok := patchProtocolArray(input)
	if ok {
		t.Fatalf("expected no patch on unrelated content")
	}
	if string(out) != string(input) {
		t.Fatalf("content must be unchanged on no-match")
	}
}

func TestPatchProtocolArrayFullPattern(t *testing.T) {
	input := []byte("const dDe=[`devtools:`,`file:`,`app:`];const x=1;")
	out, ok := patchProtocolArray(input)
	if !ok {
		t.Fatalf("expected patch to apply")
	}
	if !strings.Contains(string(out), "`chrome-extension:`") {
		t.Fatalf("expected backtick chrome-extension entry, got: %s", out)
	}
}
