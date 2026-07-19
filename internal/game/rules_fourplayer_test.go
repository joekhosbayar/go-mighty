package game

import (
	"errors"
	"testing"
)

func fourPlayerBidding(t *testing.T) *Game {
	t.Helper()
	g := NewWithConfig("bid4", GameConfig{NumPlayers: 4, AllowJokerPartner: true, FailDist: FailEqualSplit})
	for i := 0; i < 4; i++ {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i, Hand: []Card{}}
	}
	g.Start()
	return g
}

func TestFourPlayerBidBelowFourteenRejected(t *testing.T) {
	g := fourPlayerBidding(t)
	err := g.ValidateMove(g.Players[0].ID, MoveBid, Bid{Points: 3, Suit: Spades})
	if !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove for Points 3 in four-player game, got %v", err)
	}
	if err := g.ValidateMove(g.Players[0].ID, MoveBid, Bid{Points: 4, Suit: Spades}); err != nil {
		t.Fatalf("Points 4 should be legal in four-player game, got %v", err)
	}
}

func TestFourPlayerBiddingEndsWhenThreePass(t *testing.T) {
	g := fourPlayerBidding(t)
	_ = g.ApplyMove(g.Players[0].ID, MoveBid, Bid{Points: 4, Suit: Spades})
	_ = g.ApplyMove(g.Players[1].ID, MovePass, nil)
	_ = g.ApplyMove(g.Players[2].ID, MovePass, nil)
	_ = g.ApplyMove(g.Players[3].ID, MovePass, nil)
	if g.Status != PhaseExchanging {
		t.Fatalf("status after three passes: got %s, want %s", g.Status, PhaseExchanging)
	}
	if g.Declarer != 0 {
		t.Errorf("declarer: got %d, want 0", g.Declarer)
	}
	if len(g.Players[0].Hand) != 13 {
		t.Errorf("declarer hand after kitty: got %d, want 13", len(g.Players[0].Hand))
	}
}

func TestFourPlayerAllPassRedeals(t *testing.T) {
	g := fourPlayerBidding(t)
	_ = g.ApplyMove(g.Players[0].ID, MovePass, nil)
	_ = g.ApplyMove(g.Players[1].ID, MovePass, nil)
	_ = g.ApplyMove(g.Players[2].ID, MovePass, nil)
	_ = g.ApplyMove(g.Players[3].ID, MovePass, nil)
	if g.Status != PhaseBidding {
		t.Fatalf("all-pass should redeal into bidding, got %s", g.Status)
	}
	if len(g.PassedPlayers) != 0 {
		t.Errorf("passed players should reset, got %d", len(g.PassedPlayers))
	}
}
