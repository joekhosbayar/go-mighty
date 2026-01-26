package game

import (
	"testing"
)

func TestGameFlow(t *testing.T) {
	// 1. Initialize Game
	g := NewGame("test-game")

	// Add players
	for i := 0; i < 5; i++ {
		p := &Player{ID: string(rune('A' + i)), Seat: i, Name: string(rune('A' + i))}
		g.Players[i] = p
	}

	// 2. Start (Deal)
	g.Start()
	if len(g.Players[0].Hand) != 10 {
		t.Errorf("Expected 10 cards, got %d", len(g.Players[0].Hand))
	}

	// 3. Bid
	// Player 0 (current turn) bids 13 S
	err := g.ValidateMove(g.Players[0].ID, MoveBid, Bid{Points: 13, Suit: Spades})
	if err != nil {
		t.Errorf("Valid bid rejected: %v", err)
	}
	g.ApplyMove(g.Players[0].ID, MoveBid, Bid{Points: 13, Suit: Spades})

	if g.CurrentBid == nil || g.CurrentBid.Points != 13 {
		t.Errorf("Bid not applied")
	}

	// Player 1 passes
	if g.CurrentTurn != 1 {
		t.Errorf("Expected turn 1, got %d", g.CurrentTurn)
	}
	err = g.ValidateMove(g.Players[1].ID, MovePass, nil)
	if err != nil {
		t.Errorf("Valid pass rejected: %v", err)
	}
	g.ApplyMove(g.Players[1].ID, MovePass, nil)

	// Simulate others pass
	g.ApplyMove(g.Players[2].ID, MovePass, nil)
	g.ApplyMove(g.Players[3].ID, MovePass, nil)

	// Player 4 bids higher?
	// Validate low bid
	err = g.ValidateMove(g.Players[4].ID, MoveBid, Bid{Points: 13, Suit: Diamonds}) // Same points, suit -> Invalid (unless NT)
	if err == nil {
		t.Errorf("Expected error for low bid")
	}

	g.ApplyMove(g.Players[4].ID, MovePass, nil)

	// Now Game should be in PhaseExchanging
	if g.Status != PhaseExchanging {
		t.Errorf("Expected Exchanging phase, got %s", g.Status)
	}

	// Check Declarer
	if g.Declarer != 0 {
		t.Errorf("Expected declarer 0, got %d", g.Declarer)
	}

	// Declarer hand should be 13 (10 + 3 kitty)
	if len(g.Players[0].Hand) != 13 {
		t.Errorf("Expected 13 cards for declarer, got %d", len(g.Players[0].Hand))
	}
}

func TestBeats(t *testing.T) {
	g := NewGame("test")
	// Scenario: Trump is Hearts. So Spades Ace is Mighty.
	g.Trump = Hearts

	mighty := Card{Suit: Spades, Rank: Ace}
	joker := Card{Suit: None, Rank: Joker}
	trumpK := Card{Suit: Hearts, Rank: King}
	clubA := Card{Suit: Clubs, Rank: Ace}
	clubK := Card{Suit: Clubs, Rank: King}

	// 1. Mighty beats Joker
	if !g.Beats(mighty, joker, Clubs) {
		t.Errorf("Mighty should beat Joker")
	}

	// 2. Joker beats Trump K
	if !g.Beats(joker, trumpK, Clubs) {
		t.Errorf("Joker should beat Trump K")
	}

	// 3. Trump K beats Lead Suit A (Club A)
	if !g.Beats(trumpK, clubA, Clubs) {
		t.Errorf("Trump should beat Lead Suit A")
	}

	// 4. Lead Suit A beats Lead Suit K
	if !g.Beats(clubA, clubK, Clubs) {
		t.Errorf("Higher rank should win in lead suit")
	}

	// 5. Lead Suit A beats Off Suit K (Diamond King) - Non Trump
	diamondK := Card{Suit: Diamonds, Rank: King}
	if !g.Beats(clubA, diamondK, Clubs) {
		t.Errorf("Lead suit should beat off suit")
	}
}
