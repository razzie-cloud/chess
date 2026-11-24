// Package opening recognizes the deepest named prefix of standard-start games.
package opening

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/razzie-cloud/chess/internal/game"
)

//go:generate go run ../../cmd/ecoprep -input source/eco.tsv -output eco.tsv

//go:embed eco.tsv
var data []byte

type Entry struct {
	Code  string `json:"code"`
	Title string `json:"title"`
}
type node struct {
	children map[string]*node
	entry    *Entry
}
type Book struct {
	root  node
	count int
}

var load = sync.OnceValue(func() *Book {
	b, err := NewBook(bytes.NewReader(data))
	if err != nil {
		panic("invalid bundled opening data")
	}
	return b
})

// Lookup intentionally suppresses titles for custom/Chess960 starts, even if
// their current position resembles a standard opening.
func Lookup(initial string, moves []string, mode string) *Entry {
	if initial != game.StartFEN || (mode != "standard" && mode != "") {
		return nil
	}
	return load().Find(moves)
}

func (b *Book) Find(moves []string) *Entry {
	n := &b.root
	var found *Entry
	for _, uci := range moves {
		child := n.children[uci]
		if child == nil {
			break
		}
		n = child
		if n.entry != nil {
			e := *n.entry
			found = &e
		}
	}
	return found
}

func NewBook(input io.Reader) (*Book, error) {
	r := csv.NewReader(input)
	r.Comma = '\t'
	r.FieldsPerRecord = 3
	header, err := r.Read()
	if err != nil || strings.Join(header, "\t") != "eco\tname\tuci" {
		return nil, errors.New("invalid normalized ECO header")
	}
	b := &Book{root: node{children: map[string]*node{}}}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(row[0]) != 3 || row[0][0] < 'A' || row[0][0] > 'E' || row[0][1] < '0' || row[0][1] > '9' || row[0][2] < '0' || row[0][2] > '9' || row[1] == "" || row[2] == "" {
			return nil, errors.New("invalid normalized ECO record")
		}
		n := &b.root
		for _, uci := range strings.Fields(row[2]) {
			if len(uci) < 4 || len(uci) > 5 {
				return nil, errors.New("invalid normalized ECO move")
			}
			if n.children[uci] == nil {
				n.children[uci] = &node{children: map[string]*node{}}
			}
			n = n.children[uci]
		}
		// Preserve the source book's deterministic last-row precedence.
		n.entry = &Entry{Code: row[0], Title: row[1]}
		b.count++
	}
	return b, nil
}

// Prepare replays every source line through the current rules library and emits
// canonical king-to-rook UCI. One invalid source record fails the whole build.
func Prepare(input io.Reader) ([]byte, int, error) {
	source, err := io.ReadAll(io.LimitReader(input, 4*1024*1024+1))
	if err != nil {
		return nil, 0, err
	}
	if len(source) > 4*1024*1024 {
		return nil, 0, errors.New("source ECO data exceeds 4 MiB")
	}
	r := csv.NewReader(bytes.NewReader(source))
	r.Comma = '\t'
	r.FieldsPerRecord = 5
	header, err := r.Read()
	if err != nil || strings.Join(header, "\t") != "eco\tname\tpgn\tuci\tepd" {
		return nil, 0, errors.New("invalid source ECO header")
	}
	var output bytes.Buffer
	w := csv.NewWriter(&output)
	w.Comma = '\t'
	if err = w.Write([]string{"eco", "name", "uci"}); err != nil {
		return nil, 0, err
	}
	count := 0
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		g, err := game.Replay(game.StartFEN, strings.Fields(row[3]), false)
		if err != nil {
			return nil, 0, fmt.Errorf("ECO source record %d (%s %s): %w", count+1, row[0], row[1], err)
		}
		moves := make([]string, len(g.Moves))
		for i, m := range g.Moves {
			moves[i] = m.UCI
		}
		if err = w.Write([]string{row[0], row[1], strings.Join(moves, " ")}); err != nil {
			return nil, 0, err
		}
		count++
	}
	w.Flush()
	if err = w.Error(); err != nil {
		return nil, 0, err
	}
	if _, err = NewBook(bytes.NewReader(output.Bytes())); err != nil {
		return nil, 0, err
	}
	return output.Bytes(), count, nil
}
