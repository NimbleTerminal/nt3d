package demo

import (
	"testing"

	"nimbleterminal.dev/ntpicture"
)

func TestParseKittyFrameEdits(t *testing.T) {
	for value, want := range map[string]ntpicture.KittyCapability{
		"auto": ntpicture.KittyCapabilityUnknown,
		"on":   ntpicture.KittyCapabilitySupported,
		"off":  ntpicture.KittyCapabilityUnsupported,
	} {
		got, err := parseKittyFrameEdits(value)
		if err != nil || got != want {
			t.Fatalf("%s: got %v, %v; want %v", value, got, err, want)
		}
	}
	if _, err := parseKittyFrameEdits("invalid"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}
