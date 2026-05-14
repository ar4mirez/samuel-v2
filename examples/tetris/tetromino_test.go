package tetris

import (
	"math/bits"
	"testing"
)

var allShapes = []Shape{
	ShapeI, ShapeO, ShapeT, ShapeS, ShapeZ, ShapeL, ShapeJ,
}

func TestRotateFourTimesReturnsOriginalBitmask(t *testing.T) {
	for _, s := range allShapes {
		original := Tetromino{Shape: s}
		rotated := original
		for i := 0; i < 4; i++ {
			rotated = Rotate(rotated)
		}
		if Bitmask(rotated) != Bitmask(original) {
			t.Errorf("shape %d: bitmask after 4 rotations = %016b, want %016b",
				s, Bitmask(rotated), Bitmask(original))
		}
	}
}

func TestRotatePreservesShape(t *testing.T) {
	for _, s := range allShapes {
		rotated := Rotate(Tetromino{Shape: s, Rotation: 2})
		if rotated.Shape != s {
			t.Errorf("Rotate changed shape %d -> %d", s, rotated.Shape)
		}
	}
}

func TestEveryRotationHasFourFilledCells(t *testing.T) {
	for _, s := range allShapes {
		for r := byte(0); r < 4; r++ {
			tet := Tetromino{Shape: s, Rotation: r}
			if count := bits.OnesCount16(Bitmask(tet)); count != 4 {
				t.Errorf("shape %d rotation %d: %d cells set, want 4",
					s, r, count)
			}
		}
	}
}

func TestIPieceBaseRotationFillsRowOne(t *testing.T) {
	got := Bitmask(Tetromino{Shape: ShapeI})
	want := uint16(0xF0)
	if got != want {
		t.Errorf("ShapeI base bitmask = %016b, want %016b", got, want)
	}
}

func TestOPieceIsRotationallyInvariant(t *testing.T) {
	base := Bitmask(Tetromino{Shape: ShapeO})
	for r := byte(1); r < 4; r++ {
		got := Bitmask(Tetromino{Shape: ShapeO, Rotation: r})
		if got != base {
			t.Errorf("ShapeO rotation %d = %016b, want %016b", r, got, base)
		}
	}
}

func TestRotateAdvancesRotationField(t *testing.T) {
	tet := Tetromino{Shape: ShapeT}
	for i := byte(0); i < 4; i++ {
		if tet.Rotation != i {
			t.Errorf("expected rotation %d at step %d, got %d",
				i, i, tet.Rotation)
		}
		tet = Rotate(tet)
	}
}

func TestBitmaskNormalizesOutOfRangeRotation(t *testing.T) {
	base := Bitmask(Tetromino{Shape: ShapeT, Rotation: 1})
	wrapped := Bitmask(Tetromino{Shape: ShapeT, Rotation: 5})
	if base != wrapped {
		t.Errorf("rotation should be modulo 4: got %016b, want %016b",
			wrapped, base)
	}
}

func TestNonOPiecesHaveDistinctRotationStates(t *testing.T) {
	for _, s := range allShapes {
		if s == ShapeO {
			continue
		}
		base := Bitmask(Tetromino{Shape: s})
		quarter := Bitmask(Tetromino{Shape: s, Rotation: 1})
		if base == quarter {
			t.Errorf("shape %d: rotation 0 and 1 share bitmask %016b",
				s, base)
		}
	}
}
