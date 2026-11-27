package ui

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

func TestPickRGBIsTheOpaqueColour(t *testing.T) {
	if got, want := PickRGB(0xD5, 0x5E, 0x00), toolkit.RGB(0xD5, 0x5E, 0x00); got != want {
		t.Errorf("PickRGB = %v, want %v", got, want)
	}
	if PickRGB(1, 2, 3) == PickRGB(3, 2, 1) {
		t.Error("the channels are swapped or dropped")
	}
}
