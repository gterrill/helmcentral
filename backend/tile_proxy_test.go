package main

import "testing"

func TestClampTileCoords_NoClampNeeded(t *testing.T) {
	z, x, y := clampTileCoords(18, 242417, 150119, 18)
	if z != 18 || x != 242417 || y != 150119 {
		t.Fatalf("unexpected clamp result: z=%d x=%d y=%d", z, x, y)
	}
}

func TestClampTileCoords_ClampByOneZoom(t *testing.T) {
	z, x, y := clampTileCoords(19, 484835, 300239, 18)
	if z != 18 || x != 242417 || y != 150119 {
		t.Fatalf("unexpected clamp result: z=%d x=%d y=%d", z, x, y)
	}
}

func TestClampTileCoords_ClampByTwoZooms(t *testing.T) {
	z, x, y := clampTileCoords(20, 969671, 600478, 18)
	if z != 18 || x != 242417 || y != 150119 {
		t.Fatalf("unexpected clamp result: z=%d x=%d y=%d", z, x, y)
	}
}
