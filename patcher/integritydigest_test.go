package patcher

import (
	"encoding/hex"
	"testing"
)

// Claude 4.4212.0 (Electron 44.9.3): the dictionary its Info.plist ships with, and the
// digest its Electron Framework holds for it, in both architectures.
func TestIntegrityDictDigest(t *testing.T) {
	dict := map[string]asarIntegrityEntry{
		"Resources/ion-dist.asar": {"SHA256", "ba7a5bcd05df7d52931a393053d070f06a3f44fb00adcbcc52e9c25ca248b866"},
		"Resources/app.asar":      {"SHA256", "2d8822229ac8cb900afb49c4bb2bd9893d7a7791e39b1308ac98e85e78ab2b69"},
	}
	const want = "479881ae68b158752618bcb7a92c417b22a968435932384f49f672398c32a7e3"

	digest := integrityDictDigest(dict)
	if got := hex.EncodeToString(digest[:]); got != want {
		t.Fatalf("integrityDictDigest = %s, want %s", got, want)
	}
}
