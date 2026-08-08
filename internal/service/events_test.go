package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/joekhosbayar/go-mighty/internal/game"
)

func dealtGame(t *testing.T) *game.Game {
	t.Helper()

	g := game.New("g1")
	for i := range 5 {
		g.Players[i] = &game.Player{ID: string(rune('a' + i)), Seat: i, IsConnected: true}
	}

	g.Start()

	return g
}

func TestRedactForHidesOtherHands(t *testing.T) {
	t.Parallel()

	g := dealtGame(t)
	ev := GameEvent{Type: EventTypeMove, MoveType: game.MovePlayCard, Version: g.Version, GameState: g}

	out := ev.RedactFor("b")
	if out.GameState == nil {
		t.Fatal("GameState should survive redaction")
	}

	if len(out.GameState.Players[1].Hand) != 10 {
		t.Errorf("viewer lost their own hand: %d cards", len(out.GameState.Players[1].Hand))
	}

	if out.GameState.Players[0].Hand != nil {
		t.Error("seat 0's hand leaked")
	}

	if out.Type != EventTypeMove || out.MoveType != game.MovePlayCard || out.Version != g.Version {
		t.Errorf("envelope metadata mangled: %+v", out)
	}
}

func TestRedactForToleratesNilState(t *testing.T) {
	t.Parallel()

	seat := 3
	out := GameEvent{Type: EventTypePlayerJoined, Seat: &seat, Version: 7}.RedactFor("a")

	if out.GameState != nil {
		t.Error("nil state should stay nil")
	}

	if out.Seat == nil || *out.Seat != 3 {
		t.Errorf("seat lost: %+v", out.Seat)
	}
}

func TestProcessMovePublishesNoMovePayload(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(GameEvent{
		Type:      EventTypeMove,
		MoveType:  game.MoveDiscard,
		PlayerID:  "a",
		Version:   3,
		GameState: dealtGame(t),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(data), `"payload"`) {
		t.Errorf("move envelope still carries a payload field\n%s", data)
	}
}
