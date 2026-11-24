package db

import _ "embed"

//go:embed seeds/puzzles.json
var puzzleSeeds []byte

func PuzzleSeeds() []byte { return append([]byte(nil), puzzleSeeds...) }
