//go:build ignore

// Generate original feedback tones with no sampled or third-party audio.
// Run from the module root: go run ./tools/generate-sounds.go (requires ffmpeg).
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
)

const rate = 22050

func main() {
	tones := []struct {
		name    string
		notes   []float64
		seconds float64
	}{
		{"move", []float64{520}, .08},
		{"capture", []float64{370, 550}, .07},
		{"check", []float64{880, 1100}, .09},
		{"illegal", []float64{220, 185}, .09},
		{"game-over", []float64{660, 550, 440}, .13},
	}
	for _, tone := range tones {
		var pcm bytes.Buffer
		for _, frequency := range tone.notes {
			for i := 0; i < int(tone.seconds*rate); i++ {
				t := float64(i) / rate
				envelope := math.Min(t/.005, 1) * math.Exp(-7*t/tone.seconds)
				sample := int16(11000 * envelope * math.Sin(2*math.Pi*frequency*t))
				if err := binary.Write(&pcm, binary.LittleEndian, sample); err != nil {
					panic(err)
				}
			}
		}
		var wav bytes.Buffer
		wav.WriteString("RIFF")
		binary.Write(&wav, binary.LittleEndian, uint32(36+pcm.Len()))
		wav.WriteString("WAVEfmt ")
		binary.Write(&wav, binary.LittleEndian, uint32(16))
		binary.Write(&wav, binary.LittleEndian, uint16(1))
		binary.Write(&wav, binary.LittleEndian, uint16(1))
		binary.Write(&wav, binary.LittleEndian, uint32(rate))
		binary.Write(&wav, binary.LittleEndian, uint32(rate*2))
		binary.Write(&wav, binary.LittleEndian, uint16(2))
		binary.Write(&wav, binary.LittleEndian, uint16(16))
		wav.WriteString("data")
		binary.Write(&wav, binary.LittleEndian, uint32(pcm.Len()))
		wav.Write(pcm.Bytes())
		target := filepath.Join("internal", "web", "static", "sounds", tone.name+".ogg")
		cmd := exec.Command("ffmpeg", "-v", "error", "-f", "wav", "-i", "pipe:0", "-map_metadata", "-1", "-c:a", "libvorbis", "-q:a", "3", "-fflags", "+bitexact", "-flags:a", "+bitexact", "-y", target)
		cmd.Stdin = &wav
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "generate", tone.name, err)
			os.Exit(1)
		}
	}
}
