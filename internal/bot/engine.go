package bot

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/razzie-cloud/chess/internal/game"
)

// A fresh process bounds crashes, hung engines and per-game state to one job.
// The worker calls this serially and always reaps the child before starting another.
func Search(ctx context.Context, binary string, g *game.Game, level int) (string, error) {
	if level < 1 || level > len(Difficulties) || g.Result != "*" {
		return "", errors.New("invalid search")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err = cmd.Start(); err != nil {
		return "", err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 4096), 64<<10)
	send := func(s string) error { _, err := io.WriteString(in, s+"\n"); return err }
	wait := func(marker string) error {
		for scan.Scan() {
			if scan.Text() == marker {
				return nil
			}
		}
		return errors.New("engine handshake failed")
	}
	if err = send("uci"); err != nil {
		return "", err
	}
	if err = wait("uciok"); err != nil {
		return "", err
	}
	d := Difficulties[level-1]
	// Always use rook-square castling, matching Chess's canonical move history,
	// including standard chess (one of the valid Chess960 starting positions).
	commands := []string{"setoption name Threads value 1", "setoption name Hash value 32", "setoption name Ponder value false", "setoption name UCI_Chess960 value true", fmt.Sprintf("setoption name Skill Level value %d", d.Skill), "ucinewgame", "isready"}
	for _, c := range commands {
		if err = send(c); err != nil {
			return "", err
		}
	}
	if err = wait("readyok"); err != nil {
		return "", err
	}
	position := "position fen " + g.InitialFEN
	if len(g.Moves) > 0 {
		position += " moves"
		for _, m := range g.Moves {
			position += " " + m.UCI
		}
	}
	if err = send(position); err != nil {
		return "", err
	}
	if err = send(fmt.Sprintf("go movetime %d", d.Millis)); err != nil {
		return "", err
	}
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) >= 2 && fields[0] == "bestmove" {
			move, err := game.LegalMove(g.Board, fields[1], g.Chess960)
			if err != nil {
				return "", errors.New("engine returned illegal move")
			}
			return move.Uci(g.Board), nil
		}
	}
	return "", errors.New("engine stopped without a move")
}
