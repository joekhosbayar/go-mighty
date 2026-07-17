package game

import (
	"errors"
	"fmt"
	"testing"
)

// jokerLeadGame returns a game where seat 0 leads trick 2 holding the Joker.
func jokerLeadGame() *Game {
	g := New("joker-test")
	for i := range 5 {
		g.Players[i] = &Player{ID: fmt.Sprintf("p%d", i), Seat: i, Hand: []Card{}, Points: []Card{}}
	}

	g.Status = PhasePlaying
	g.Declarer = 0
	g.Trump = Spades
	g.Tricks = []Trick{
		{Cards: make([]PlayedCard, 5), LeadSuit: Clubs, Winner: 0},
		{Cards: []PlayedCard{}},
	}
	g.CurrentTurn = 0
	g.Players[0].Hand = []Card{{Suit: None, Rank: Joker}, {Suit: Clubs, Rank: Two}}
	g.Players[1].Hand = []Card{{Suit: Hearts, Rank: King}, {Suit: Clubs, Rank: Three}}

	return g
}

func TestJokerLeadRequiresCalledSuit(t *testing.T) {
	t.Parallel()

	g := jokerLeadGame()
	move := PlayCardMove{Card: Card{Suit: None, Rank: Joker}}

	if err := g.ValidateMove("p0", MovePlayCard, move); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("joker lead without called_suit must be rejected, got %v", err)
	}
}

func TestJokerLeadSetsLeadSuitAndForcesFollow(t *testing.T) {
	t.Parallel()

	g := jokerLeadGame()
	move := PlayCardMove{Card: Card{Suit: None, Rank: Joker}, CalledSuit: Hearts}

	if err := g.ValidateMove("p0", MovePlayCard, move); err != nil {
		t.Fatalf("validate joker lead: %v", err)
	}

	if err := g.ApplyMove("p0", MovePlayCard, move); err != nil {
		t.Fatalf("apply joker lead: %v", err)
	}

	if got := g.Tricks[1].LeadSuit; got != Hearts {
		t.Fatalf("lead suit not taken from called_suit: %s", got)
	}

	// Seat 1 holds a heart, so a club is an illegal follow.
	follow := PlayCardMove{Card: Card{Suit: Clubs, Rank: Three}}
	if err := g.ValidateMove("p1", MovePlayCard, follow); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("must follow the called suit, got %v", err)
	}

	legal := PlayCardMove{Card: Card{Suit: Hearts, Rank: King}}
	if err := g.ValidateMove("p1", MovePlayCard, legal); err != nil {
		t.Fatalf("heart follow must be legal: %v", err)
	}
}

func TestCalledSuitRejectedOffJokerLead(t *testing.T) {
	t.Parallel()

	g := jokerLeadGame()

	// Non-joker lead with called_suit.
	lead := PlayCardMove{Card: Card{Suit: Clubs, Rank: Two}, CalledSuit: Hearts}
	if err := g.ValidateMove("p0", MovePlayCard, lead); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("called_suit on a non-joker lead must be rejected, got %v", err)
	}

	// Following with called_suit.
	if err := g.ApplyMove("p0", MovePlayCard, PlayCardMove{Card: Card{Suit: Clubs, Rank: Two}}); err != nil {
		t.Fatalf("setup lead: %v", err)
	}

	follow := PlayCardMove{Card: Card{Suit: Clubs, Rank: Three}, CalledSuit: Hearts}
	if err := g.ValidateMove("p1", MovePlayCard, follow); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("called_suit while following must be rejected, got %v", err)
	}
}
