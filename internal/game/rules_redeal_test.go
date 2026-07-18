package game

import (
	"fmt"
	"testing"
)

func TestAllPassRedealsInsteadOfFinishing(t *testing.T) {
	t.Parallel()

	g := New("redeal-test")
	for i := range 5 {
		g.Players[i] = &Player{ID: fmt.Sprintf("p%d", i), Seat: i, Hand: []Card{}, Points: []Card{}}
	}

	g.Start()

	firstHands := make([][]Card, 5)
	for i, p := range g.Players {
		firstHands[i] = append([]Card{}, p.Hand...)
	}

	versionBefore := g.Version

	for i := range 5 {
		playerID := fmt.Sprintf("p%d", i)
		if err := g.ValidateMove(playerID, MovePass, nil); err != nil {
			t.Fatalf("pass %d validate: %v", i, err)
		}

		if err := g.ApplyMove(playerID, MovePass, nil); err != nil {
			t.Fatalf("pass %d apply: %v", i, err)
		}
	}

	if g.Status != PhaseBidding {
		t.Fatalf("expected fresh bidding phase after all-pass, got %s", g.Status)
	}

	if len(g.PassedPlayers) != 0 {
		t.Fatalf("passes must be cleared, got %v", g.PassedPlayers)
	}

	if g.CurrentBid != nil || g.Declarer != -1 || g.Contract != nil {
		t.Fatalf("bidding state must be reset: bid=%v declarer=%d contract=%v", g.CurrentBid, g.Declarer, g.Contract)
	}

	for i, p := range g.Players {
		if len(p.Hand) != 10 {
			t.Fatalf("player %d has %d cards after redeal", i, len(p.Hand))
		}
	}

	if len(g.Kitty) != 3 {
		t.Fatalf("kitty must be redealt with 3 cards, got %d", len(g.Kitty))
	}

	if g.Version <= versionBefore {
		t.Fatalf("version must advance across redeal: %d -> %d", versionBefore, g.Version)
	}

	afterHands := make([][]Card, 5)
	for i, p := range g.Players {
		afterHands[i] = append([]Card{}, p.Hand...)
	}

	before := fmt.Sprint(firstHands)
	after := fmt.Sprint(afterHands)

	if before == after {
		t.Fatal("redeal did not reshuffle hands")
	}
}
