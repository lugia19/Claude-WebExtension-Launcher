package patcher

import (
	"strings"
	"testing"
)

// Excerpts from Claude 2.16120.0: the token check in index.pre.js and in a chunk, and
// an inline use of the variable that must be left alone.
const (
	devModeCheckPre   = `var gZ=3e5;function vZ(e){let t=globalThis;t[_Z]??=process.env.CLAUDE_E2E_PKCE_LOG==="1"&&(e||yZ())}function yZ(){let e=process.env.CLAUDE_CDP_AUTH,t=process.env.CLAUDE_USER_DATA_DIR;if(!e||!t)return!1;return!0}`
	devModeCheckChunk = `var rze="x";function ize(){return globalThis[rze]===!0}function aze(){let e=process.env.CLAUDE_CDP_AUTH,t=process.env.CLAUDE_USER_DATA_DIR;if(!e||!t)return!1}`
	devModeInlineUse  = `function q(){return{getMergedConfig:h6i,openDeviceCodeWindowForE2e:async()=>{if(!process.env.CLAUDE_CDP_AUTH)return;e()}}}`
)

func TestPatchDevModeGate(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		patched bool
	}{
		{
			name:    "index.pre.js check",
			in:      devModeCheckPre,
			want:    strings.Replace(devModeCheckPre, "function yZ(){", "function yZ(){"+devModeGate, 1),
			patched: true,
		},
		{
			name:    "chunk check",
			in:      devModeCheckChunk,
			want:    strings.Replace(devModeCheckChunk, "function aze(){", "function aze(){"+devModeGate, 1),
			patched: true,
		},
		{
			name:    "both in one file",
			in:      devModeCheckPre + devModeCheckChunk,
			want:    strings.Replace(strings.Replace(devModeCheckPre+devModeCheckChunk, "function yZ(){", "function yZ(){"+devModeGate, 1), "function aze(){", "function aze(){"+devModeGate, 1),
			patched: true,
		},
		{
			name:    "inline use left alone",
			in:      devModeInlineUse,
			want:    devModeInlineUse,
			patched: false,
		},
		{
			name:    "no check in chunk",
			in:      `something entirely different`,
			want:    `something entirely different`,
			patched: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, patched := patchDevModeGate([]byte(c.in))
			if patched != c.patched {
				t.Errorf("patched = %v, want %v", patched, c.patched)
			}
			if string(got) != c.want {
				t.Errorf("got\n  %s\nwant\n  %s", got, c.want)
			}
			// Patching again changes nothing.
			if again, patched := patchDevModeGate(got); patched || string(again) != string(got) {
				t.Errorf("second pass changed the content (patched = %v)", patched)
			}
		})
	}
}
