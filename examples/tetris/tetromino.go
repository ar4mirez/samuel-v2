package tetris

// Shape identifies one of the seven canonical tetrominoes.
type Shape byte

// Canonical shape identifiers. The integer value plus one matches the
// palette index used by Cell (ShapeI maps to Cell value 1, and so on).
const (
	ShapeI Shape = iota
	ShapeO
	ShapeT
	ShapeS
	ShapeZ
	ShapeL
	ShapeJ
)

// rotationCount is the number of rotation states stored per piece.
const rotationCount = 4

// Tetromino is one of the seven shapes captured in a specific rotation.
type Tetromino struct {
	Shape    Shape
	Rotation byte
}

// Bitmask returns the 4×4 occupancy bitmask of t. Bit (row*4 + col) is set
// when the corresponding bounding-box cell is filled.
func Bitmask(t Tetromino) uint16 {
	return rotations[t.Shape][t.Rotation%rotationCount]
}

// Rotate advances t by one quarter-turn clockwise. Four successive Rotate
// calls return a tetromino whose bitmask matches the original byte-for-byte.
func Rotate(t Tetromino) Tetromino {
	return Tetromino{
		Shape:    t.Shape,
		Rotation: (t.Rotation + 1) % rotationCount,
	}
}

// rotations holds the four rotation bitmasks for each shape. Consumers must
// not index this table directly — go through Rotate and Bitmask instead.
var rotations = [7][rotationCount]uint16{
	ShapeI: {
		mask("....", "XXXX", "....", "...."),
		mask("..X.", "..X.", "..X.", "..X."),
		mask("....", "....", "XXXX", "...."),
		mask(".X..", ".X..", ".X..", ".X.."),
	},
	ShapeO: {
		mask(".XX.", ".XX.", "....", "...."),
		mask(".XX.", ".XX.", "....", "...."),
		mask(".XX.", ".XX.", "....", "...."),
		mask(".XX.", ".XX.", "....", "...."),
	},
	ShapeT: {
		mask(".X..", "XXX.", "....", "...."),
		mask(".X..", ".XX.", ".X..", "...."),
		mask("....", "XXX.", ".X..", "...."),
		mask(".X..", "XX..", ".X..", "...."),
	},
	ShapeS: {
		mask(".XX.", "XX..", "....", "...."),
		mask(".X..", ".XX.", "..X.", "...."),
		mask("....", ".XX.", "XX..", "...."),
		mask("X...", "XX..", ".X..", "...."),
	},
	ShapeZ: {
		mask("XX..", ".XX.", "....", "...."),
		mask("..X.", ".XX.", ".X..", "...."),
		mask("....", "XX..", ".XX.", "...."),
		mask(".X..", "XX..", "X...", "...."),
	},
	ShapeL: {
		mask("..X.", "XXX.", "....", "...."),
		mask(".X..", ".X..", ".XX.", "...."),
		mask("....", "XXX.", "X...", "...."),
		mask("XX..", ".X..", ".X..", "...."),
	},
	ShapeJ: {
		mask("X...", "XXX.", "....", "...."),
		mask(".XX.", ".X..", ".X..", "...."),
		mask("....", "XXX.", "..X.", "...."),
		mask(".X..", ".X..", "XX..", "...."),
	},
}

// mask packs four 4-character rows into a 16-bit bitmask. Any non-'.'
// character is treated as a filled cell.
func mask(r0, r1, r2, r3 string) uint16 {
	var m uint16
	for i, row := range [...]string{r0, r1, r2, r3} {
		for j := 0; j < 4; j++ {
			if j < len(row) && row[j] != '.' {
				m |= 1 << (i*4 + j)
			}
		}
	}
	return m
}
