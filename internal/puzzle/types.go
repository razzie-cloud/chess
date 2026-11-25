// Package puzzle prepares immutable, bounded mate proofs. It never searches during play.
package puzzle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const FormatVersion = 1
const CompilerVersion = "single-fork-v2"

type Manifest struct {
	Version     int                `json:"version"`
	Collections []CollectionSource `json:"collections"`
}

type CollectionSource struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Entries     []Source `json:"entries"`
}

type Source struct {
	Slot       int    `json:"slot"`
	FEN        string `json:"fen"`
	Solver     string `json:"solver"`
	Mate       int    `json:"mate"`
	Title      string `json:"title"`
	Provenance string `json:"provenance"`
	License    string `json:"license"`
	Publish    bool   `json:"publish"`
}

type Bundle struct {
	Version     int          `json:"version"`
	Compiler    string       `json:"compiler"`
	Collections []Collection `json:"collections"`
}

type Collection struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Puzzles     []Definition `json:"puzzles"`
}

type Definition struct {
	Slot       int    `json:"slot"`
	FEN        string `json:"fen"`
	Solver     string `json:"solver"`
	Mate       int    `json:"mate"`
	Title      string `json:"title"`
	Provenance string `json:"provenance"`
	License    string `json:"license"`
	Digest     string `json:"digest"`
	Forks      int    `json:"forks"`
	Lines      int    `json:"lines"`
	Decisions  int    `json:"decisions"`
	Root       *Node  `json:"root"`
}

// Node describes one prescribed solver decision. Empty Replies means that Move mates.
type Node struct {
	FEN     string  `json:"fen"`
	Move    string  `json:"move"`
	After   string  `json:"after"`
	Replies []Reply `json:"replies,omitempty"`
}

type Reply struct {
	Move string `json:"move"`
	FEN  string `json:"fen"`
	Next *Node  `json:"next"`
}

type Limits struct {
	MaxMate     int
	MaxNodes    int
	MaxDuration time.Duration
	MaxBytes    int
}

func DefaultLimits() Limits {
	return Limits{MaxMate: 5, MaxNodes: 500000, MaxDuration: 5 * time.Second, MaxBytes: 2 << 20}
}

type Report struct {
	Collection  string   `json:"collection,omitempty"`
	Slot        int      `json:"slot"`
	Status      string   `json:"status"`
	Reason      string   `json:"reason,omitempty"`
	FEN         string   `json:"fen,omitempty"`
	Path        []string `json:"path,omitempty"`
	Mate        int      `json:"proven_mate,omitempty"`
	Forks       int      `json:"forks"`
	ForkPath    []string `json:"fork_path,omitempty"`
	ForkReplies int      `json:"fork_replies,omitempty"`
	Lines       int      `json:"complete_lines"`
	Decisions   int      `json:"solver_decisions"`
	Nodes       int      `json:"search_nodes"`
	// Elapsed is report-only: compiled artifacts exclude machine-dependent timing.
	ElapsedMS int64 `json:"elapsed_ms"`
}

// ContentDigest excludes display metadata; an edition's position and solution are immutable.
func ContentDigest(d Definition) string {
	data, _ := json.Marshal(struct {
		Version int    `json:"version"`
		Slot    int    `json:"slot"`
		FEN     string `json:"fen"`
		Solver  string `json:"solver"`
		Mate    int    `json:"mate"`
		Root    *Node  `json:"root"`
	}{FormatVersion, d.Slot, d.FEN, d.Solver, d.Mate, d.Root})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Lookup follows a complete canonical history, never a FEN supplied by a browser.
func Lookup(d Definition, path []string) (*Node, error) {
	if len(path) > 2*d.Mate || len(path)%2 != 0 {
		return nil, fmt.Errorf("invalid source path")
	}
	n := d.Root
	for i := 0; i < len(path); i += 2 {
		if n == nil || n.Move != path[i] {
			return nil, fmt.Errorf("invalid source path")
		}
		var next *Node
		for _, r := range n.Replies {
			if r.Move == path[i+1] {
				next = r.Next
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("invalid source path")
		}
		n = next
	}
	if n == nil {
		return nil, fmt.Errorf("invalid source path")
	}
	return n, nil
}

func ValidCollectionID(s string) bool {
	if len(s) < 1 || len(s) > 80 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return !strings.HasPrefix(s, "-")
}
