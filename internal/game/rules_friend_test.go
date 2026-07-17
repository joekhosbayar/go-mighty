package game

import (
	"errors"
	"fmt"
	"testing"
)

// callingGame returns a 5-player game in the calling phase with seat 0 as declarer.
func callingGame() *Game {
	g := New("friend-test")
	for i := range 5 {
		g.Players[i] = &Player{ID: fmt.Sprintf("p%d", i), Name: fmt.Sprintf("P%d", i), Seat: i, Hand: []Card{}, Points: []Card{}}
	}

	g.Status = PhaseCalling
	g.Declarer = 0
	g.CurrentTurn = 0
	g.Trump = Spades
	g.Contract = &Bid{PlayerID: "p0", Points: 7, Suit: Spades}

	return g
}

func TestCallPartnerWithCardSetsPartnerCard(t *testing.T) {
	t.Parallel()

	g := callingGame()
	move := CallPartnerMove{Card: &Card{Suit: Diamonds, Rank: Ace}}

	if err := g.ValidateMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if err := g.ApplyMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerCard == nil || g.PartnerCard.Rank != Ace || g.PartnerCard.Suit != Diamonds {
		t.Fatalf("partner card not stored: %+v", g.PartnerCard)
	}

	if g.IsNoFriend {
		t.Fatal("IsNoFriend must stay false when a card is called")
	}

	if g.Status != PhasePlaying {
		t.Fatalf("expected playing, got %s", g.Status)
	}
}

func TestCallPartnerNoFriendSetsFlagAndSkipsCard(t *testing.T) {
	t.Parallel()

	g := callingGame()
	move := CallPartnerMove{NoFriend: true}

	if err := g.ValidateMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if err := g.ApplyMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if !g.IsNoFriend {
		t.Fatal("IsNoFriend not set")
	}

	if g.PartnerCard != nil {
		t.Fatalf("partner card must be nil, got %+v", g.PartnerCard)
	}

	if g.Status != PhasePlaying {
		t.Fatalf("expected playing, got %s", g.Status)
	}
}

func TestCallPartnerRejectsBothAndNeither(t *testing.T) {
	t.Parallel()

	both := CallPartnerMove{Card: &Card{Suit: Hearts, Rank: Ace}, NoFriend: true}
	if err := callingGame().ValidateMove("p0", MoveCallPartner, both); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("both card and no_friend must be rejected, got %v", err)
	}

	neither := CallPartnerMove{}
	if err := callingGame().ValidateMove("p0", MoveCallPartner, neither); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("empty call must be rejected, got %v", err)
	}
}

func TestCallPartnerLegacyBareCardStillAccepted(t *testing.T) {
	t.Parallel()

	g := callingGame()
	card := Card{Suit: Hearts, Rank: Ace}

	if err := g.ValidateMove("p0", MoveCallPartner, card); err != nil {
		t.Fatalf("validate legacy card: %v", err)
	}

	if err := g.ApplyMove("p0", MoveCallPartner, card); err != nil {
		t.Fatalf("apply legacy card: %v", err)
	}

	if g.PartnerCard == nil || g.PartnerCard.Suit != Hearts {
		t.Fatalf("legacy card not stored: %+v", g.PartnerCard)
	}
}
