// ecoprep deterministically normalizes the pinned legacy opening book.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/razzie-cloud/chess/internal/opening"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	input := flag.String("input", "internal/opening/source/eco.tsv", "source ECO TSV")
	output := flag.String("output", "internal/opening/eco.tsv", "canonical output TSV")
	flag.Parse()
	f, err := os.Open(*input)
	if err != nil {
		return err
	}
	defer f.Close()
	data, count, err := opening.Prepare(f)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(*output), ".eco-*.tsv")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), *output); err != nil {
		return err
	}
	fmt.Printf("Normalized %d ECO records\n", count)
	return nil
}
