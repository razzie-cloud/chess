// puzzleprep is an offline content tool; it has no database or network configuration.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/razzie-cloud/chess/internal/puzzle"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func readJSON(name string, out any) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > 32<<20 {
		return errors.New("input exceeds 32 MiB")
	}
	d := json.NewDecoder(io.LimitReader(f, 32<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("input must contain exactly one JSON document under 32 MiB")
	}
	return nil
}

func writeJSON(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.CreateTemp(filepath.Dir(name), ".puzzleprep-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0644); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), name)
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: puzzleprep add|import|compile|verify|screen [flags]")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "compile":
		input := fs.String("input", "puzzles/source.json", "source manifest")
		output := fs.String("output", "internal/db/seeds/puzzles.json", "compiled bundle")
		reportFile := fs.String("report", "puzzles/preparation-report.json", "per-entry report")
		limit := puzzle.DefaultLimits()
		fs.IntVar(&limit.MaxNodes, "nodes", limit.MaxNodes, "maximum search nodes per entry")
		fs.DurationVar(&limit.MaxDuration, "timeout", limit.MaxDuration, "maximum search duration per entry")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *input == *output || *reportFile == *output || *reportFile == *input {
			return errors.New("input, output and report must be distinct files")
		}
		var m puzzle.Manifest
		if err := readJSON(*input, &m); err != nil {
			return err
		}
		bundle, reports, err := puzzle.CompileManifest(context.Background(), m, limit)
		if writeErr := writeJSON(*reportFile, reports); writeErr != nil {
			return writeErr
		}
		if err != nil {
			return err
		}
		if err := writeJSON(*output, bundle); err != nil {
			return err
		}
		fmt.Printf("Verified %d collections; wrote %s\n", len(bundle.Collections), *output)
		return nil
	case "verify":
		input := fs.String("input", "internal/db/seeds/puzzles.json", "compiled bundle")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		var b puzzle.Bundle
		if err := readJSON(*input, &b); err != nil {
			return err
		}
		if err := puzzle.Verify(b); err != nil {
			return err
		}
		fmt.Printf("Verified %d collections\n", len(b.Collections))
		return nil
	case "add", "import":
		manifest := fs.String("manifest", "puzzles/source.json", "source manifest")
		collection := fs.String("collection", "", "permanent collection edition")
		slot := fs.Int("slot", -1, "permanent slot 0..255 (first slot for import)")
		mate := fs.Int("mate", puzzle.DefaultLimits().MaxMate, "maximum solver moves to search (1..5); mate length is calculated")
		fen := fs.String("fen", "", "six-field FEN for add")
		input := fs.String("input", "", "newline-FEN file for import")
		title := fs.String("title", "", "puzzle title")
		provenance := fs.String("provenance", "", "source and attribution")
		license := fs.String("license", "", "source license; review before publication")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if !puzzle.ValidCollectionID(*collection) || *slot < 0 || *slot > 255 || *mate < 1 || *mate > 5 {
			return errors.New("collection, slot in 0..255 and search limit in 1..5 are required")
		}
		m := puzzle.Manifest{Version: puzzle.FormatVersion}
		if err := readJSON(*manifest, &m); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		index := -1
		for i, c := range m.Collections {
			if c.ID == *collection {
				index = i
				break
			}
		}
		if index < 0 {
			m.Collections = append(m.Collections, puzzle.CollectionSource{ID: *collection, Title: *collection})
			index = len(m.Collections) - 1
		}
		fens := []string{*fen}
		if args[0] == "import" {
			var err error
			fens, err = readFENs(*input)
			if err != nil {
				return err
			}
		}
		if len(fens) == 0 || *slot+len(fens) > 256 {
			return errors.New("import would exceed permanent collection slots")
		}
		for i, f := range fens {
			src := puzzle.Source{Slot: *slot + i, FEN: f, Solver: solver(f), Mate: *mate, Title: *title, Provenance: *provenance, License: *license, Publish: false}
			if err := puzzle.CheckSource(src); err != nil {
				return fmt.Errorf("source entry %d: %w", i+1, err)
			}
			for _, existing := range m.Collections[index].Entries {
				if existing.Slot == src.Slot {
					return fmt.Errorf("slot %d is already assigned", src.Slot)
				}
			}
			depth, err := puzzle.FindMate(context.Background(), src, puzzle.DefaultLimits())
			if err != nil {
				return fmt.Errorf("source entry %d: %w", i+1, err)
			}
			src.Mate = depth
			m.Collections[index].Entries = append(m.Collections[index].Entries, src)
		}
		return writeJSON(*manifest, m)
	case "screen":
		input := fs.String("input", "", "newline-FEN input")
		collection := fs.String("collection", "", "explicit collection edition")
		mate := fs.Int("mate", 0, "explicit solver move limit 1..5")
		reportFile := fs.String("report", "puzzles/screening-report.json", "screen report")
		limit := puzzle.DefaultLimits()
		fs.IntVar(&limit.MaxNodes, "nodes", limit.MaxNodes, "maximum nodes per source")
		fs.DurationVar(&limit.MaxDuration, "timeout", 5*time.Second, "maximum duration per source")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if !puzzle.ValidCollectionID(*collection) || *mate < 1 || *mate > 5 {
			return errors.New("collection and explicit mate limit in 1..5 are required")
		}
		fens, err := readFENs(*input)
		if err != nil {
			return err
		}
		if len(fens) > 256 {
			return errors.New("source exceeds 256 slots; explicitly split collections")
		}
		var reports []puzzle.Report
		accepted := 0
		for i, f := range fens {
			_, r, err := puzzle.Compile(context.Background(), puzzle.Source{Slot: i, FEN: f, Solver: solver(f), Mate: *mate, Provenance: *input, License: "screening only; publication requires source license review"}, limit)
			r.Collection = *collection
			reports = append(reports, r)
			if err == nil {
				accepted++
			}
		}
		if err := writeJSON(*reportFile, reports); err != nil {
			return err
		}
		fmt.Printf("Screened %d: accepted %d, rejected %d\n", len(fens), accepted, len(fens)-accepted)
		return nil
	default:
		return errors.New("unknown command; use add, import, compile, verify or screen")
	}
}

func solver(fen string) string {
	fields := strings.Fields(fen)
	if len(fields) > 1 {
		return fields[1]
	}
	return ""
}
func readFENs(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 1<<20 {
		return nil, errors.New("FEN input exceeds 1 MiB")
	}
	s := bufio.NewScanner(io.LimitReader(f, 1<<20))
	s.Buffer(make([]byte, 1024), 1024)
	var out []string
	for s.Scan() {
		if line := strings.TrimSpace(s.Text()); line != "" {
			out = append(out, line)
			if len(out) > 256 {
				return nil, errors.New("at most 256 source positions per collection")
			}
		}
	}
	return out, s.Err()
}
