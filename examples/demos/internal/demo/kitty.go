package demo

import (
	"fmt"

	"nimbleterminal.dev/ntpicture"
)

func parseKittyFrameEdits(value string) (ntpicture.KittyCapability, error) {
	switch value {
	case "auto":
		return ntpicture.KittyCapabilityUnknown, nil
	case "on":
		return ntpicture.KittyCapabilitySupported, nil
	case "off":
		return ntpicture.KittyCapabilityUnsupported, nil
	default:
		return ntpicture.KittyCapabilityUnknown, fmt.Errorf("invalid kitty-frame-edits %q: want auto, on, or off", value)
	}
}
