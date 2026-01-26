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

// TestRipperCard tests the Ripper card identification
func TestRipperCard(t *testing.T) {
	g := NewGame("test-ripper")
	g.Trump = Hearts

	// Ripper is Clubs 3 (unless Clubs is trump, then Spades 3)
	ripper := Card{Suit: Clubs, Rank: Three}
	mighty := Card{Suit: Spades, Rank: Ace}

	// Test IsRipper
	if !g.IsRipper(ripper) {
		t.Errorf("Clubs 3 should be Ripper when trump is Hearts")
	}

	// Ripper should NOT beat Mighty
	if g.Beats(ripper, mighty, Clubs) {
		t.Errorf("Ripper should not beat Mighty")
	}

	// Note: Ripper beating Joker requires trick context (whether Ripper was led)
	// which is not implemented in the simple Beats() function yet.
	// This would need to be tested at the trick resolution level.
	
	// Test when Clubs is trump - Spades 3 should be Ripper
	g.Trump = Clubs
	spadesRipper := Card{Suit: Spades, Rank: Three}
	
	if !g.IsRipper(spadesRipper) {
		t.Errorf("Spades 3 should be Ripper when trump is Clubs")
	}
	
	if g.IsRipper(ripper) {
		t.Errorf("Clubs 3 should NOT be Ripper when trump is Clubs")
	}
}

// TestMightyCard tests the Mighty card special rules
func TestMightyCard(t *testing.T) {
	g := NewGame("test-mighty")
	g.Trump = Hearts

	// Mighty is Spades Ace when trump is NOT spades
	mighty := Card{Suit: Spades, Rank: Ace}
	
	if !g.IsMighty(mighty) {
		t.Errorf("Spades Ace should be Mighty when trump is Hearts")
	}

	// When trump is Spades, Mighty should be Diamonds Ace
	g.Trump = Spades
	mightyDiamonds := Card{Suit: Diamonds, Rank: Ace}
	
	if !g.IsMighty(mightyDiamonds) {
		t.Errorf("Diamonds Ace should be Mighty when trump is Spades")
	}
	
	if g.IsMighty(mighty) {
		t.Errorf("Spades Ace should NOT be Mighty when trump is Spades")
	}
}

// TestCardComparison tests rank ordering
func TestCardComparison(t *testing.T) {
	g := NewGame("test-ranks")
	g.Trump = Hearts

	// Test same suit comparisons
	aceHearts := Card{Suit: Hearts, Rank: Ace}
	kingHearts := Card{Suit: Hearts, Rank: King}
	
	if !g.Beats(aceHearts, kingHearts, Hearts) {
		t.Errorf("Ace should beat King in same suit")
	}

	// Test trump vs non-trump
	twoHearts := Card{Suit: Hearts, Rank: Two}
	aceClubs := Card{Suit: Clubs, Rank: Ace}
	
	if !g.Beats(twoHearts, aceClubs, Clubs) {
		t.Errorf("Trump 2 should beat non-trump Ace")
	}
}

// TestBiddingPhase tests bidding logic and validation
func TestBiddingPhase(t *testing.T) {
	g := NewGame("test-bidding")
	
	// Add players
	for i := 0; i < 5; i++ {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i, Name: string(rune('A' + i))}
	}
	
	g.Start()

	// Test minimum bid
	lowBid := Bid{Points: 10, Suit: Spades}
	err := g.ValidateMove(g.Players[0].ID, MoveBid, lowBid)
	if err == nil {
		t.Errorf("Bid below 13 should be rejected")
	}

	// Test valid first bid
	validBid := Bid{Points: 13, Suit: Spades}
	err = g.ValidateMove(g.Players[0].ID, MoveBid, validBid)
	if err != nil {
		t.Errorf("Valid bid should be accepted: %v", err)
	}

	g.ApplyMove(g.Players[0].ID, MoveBid, validBid)

	// Test that next bid must be higher
	sameBid := Bid{Points: 13, Suit: Hearts}
	err = g.ValidateMove(g.Players[1].ID, MoveBid, sameBid)
	if err == nil {
		t.Errorf("Bid with same points should be rejected")
	}

	// Test higher bid
	higherBid := Bid{Points: 14, Suit: Hearts}
	err = g.ValidateMove(g.Players[1].ID, MoveBid, higherBid)
	if err != nil {
		t.Errorf("Higher bid should be accepted: %v", err)
	}
}

// TestNoTrumpBid tests no trump bidding
func TestNoTrumpBid(t *testing.T) {
	g := NewGame("test-notrump")
	
	for i := 0; i < 5; i++ {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i, Name: string(rune('A' + i))}
	}
	
	g.Start()

	// No trump bid should be valid
	noTrumpBid := Bid{Points: 13, Suit: None, IsNoTrump: true}
	err := g.ValidateMove(g.Players[0].ID, MoveBid, noTrumpBid)
	if err != nil {
		t.Errorf("No trump bid should be valid: %v", err)
	}
}

// TestDiscardPhase tests card discarding after winning bid
func TestDiscardPhase(t *testing.T) {
	g := NewGame("test-discard")
	
	for i := 0; i < 5; i++ {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i, Name: string(rune('A' + i))}
	}
	
	g.Start()
	
	// Fast forward through bidding
	g.ApplyMove(g.Players[0].ID, MoveBid, Bid{Points: 13, Suit: Spades})
	for i := 1; i < 5; i++ {
		g.ApplyMove(g.Players[i].ID, MovePass, nil)
	}

	// Now in PhaseExchanging
	if g.Status != PhaseExchanging {
		t.Errorf("Expected PhaseExchanging, got %s", g.Status)
	}

	// Declarer should have 13 cards
	declarer := g.Players[g.Declarer]
	if len(declarer.Hand) != 13 {
		t.Errorf("Declarer should have 13 cards, got %d", len(declarer.Hand))
	}

	// Test discarding wrong number of cards
	twoCards := []Card{declarer.Hand[0], declarer.Hand[1]}
	err := g.ValidateMove(declarer.ID, MoveDiscard, twoCards)
	if err == nil {
		t.Errorf("Should reject discarding wrong number of cards")
	}

	// Test discarding 3 cards
	threeCards := []Card{declarer.Hand[0], declarer.Hand[1], declarer.Hand[2]}
	err = g.ValidateMove(declarer.ID, MoveDiscard, threeCards)
	if err != nil {
		t.Errorf("Should accept discarding 3 cards: %v", err)
	}
}

// TestCallPartnerPhase tests partner calling logic
func TestCallPartnerPhase(t *testing.T) {
	g := NewGame("test-partner")
	
	for i := 0; i < 5; i++ {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i, Name: string(rune('A' + i))}
	}
	
	g.Start()
	
	// Fast forward to PhaseExchanging
	g.ApplyMove(g.Players[0].ID, MoveBid, Bid{Points: 13, Suit: Spades})
	for i := 1; i < 5; i++ {
		g.ApplyMove(g.Players[i].ID, MovePass, nil)
	}

	// Discard 3 cards
	declarer := g.Players[g.Declarer]
	threeCards := []Card{declarer.Hand[0], declarer.Hand[1], declarer.Hand[2]}
	g.ApplyMove(declarer.ID, MoveDiscard, threeCards)

	// Now in PhaseCalling
	if g.Status != PhaseCalling {
		t.Errorf("Expected PhaseCalling, got %s", g.Status)
	}

	// Call a card (partner card)
	partnerCard := Card{Suit: Hearts, Rank: Ace}
	err := g.ValidateMove(declarer.ID, MoveCallPartner, partnerCard)
	if err != nil {
		t.Errorf("Should accept valid partner call: %v", err)
	}
}

// TestDeckShuffleAndDeal tests deck operations
func TestDeckShuffleAndDeal(t *testing.T) {
	deck := NewDeck()
	
	// Test deck size
	if len(deck) != 53 {
		t.Errorf("Deck should have 53 cards, got %d", len(deck))
	}

	// Test shuffle doesn't lose cards
	deck.Shuffle()
	if len(deck) != 53 {
		t.Errorf("Shuffled deck should still have 53 cards, got %d", len(deck))
	}

	// Test deal
	hands, kitty := deck.Deal()
	
	if len(kitty) != 3 {
		t.Errorf("Kitty should have 3 cards, got %d", len(kitty))
	}

	for i, hand := range hands {
		if len(hand) != 10 {
			t.Errorf("Hand %d should have 10 cards, got %d", i, len(hand))
		}
	}
}

// TestCardString tests card string representation
func TestCardString(t *testing.T) {
	tests := []struct {
		card     Card
		expected string
	}{
		{Card{Suit: Spades, Rank: Ace}, "sA"},
		{Card{Suit: Hearts, Rank: King}, "hK"},
		{Card{Suit: Clubs, Rank: Ten}, "c10"},
		{Card{Suit: None, Rank: Joker}, "Joker"},
		{Card{Suit: Diamonds, Rank: Two}, "d2"},
	}

	for _, tt := range tests {
		result := tt.card.String()
		if result != tt.expected {
			t.Errorf("Card.String() = %s, expected %s", result, tt.expected)
		}
	}
}

// TestIsPointCard tests point card identification
func TestIsPointCard(t *testing.T) {
	pointCards := []Card{
		{Suit: Spades, Rank: Ace},
		{Suit: Hearts, Rank: King},
		{Suit: Clubs, Rank: Queen},
		{Suit: Diamonds, Rank: Jack},
		{Suit: Spades, Rank: Ten},
	}

	nonPointCards := []Card{
		{Suit: Spades, Rank: Nine},
		{Suit: Hearts, Rank: Two},
		{Suit: Clubs, Rank: Five},
	}

	for _, card := range pointCards {
		if !card.IsPointCard() {
			t.Errorf("Card %s should be a point card", card)
		}
	}

	for _, card := range nonPointCards {
		if card.IsPointCard() {
			t.Errorf("Card %s should not be a point card", card)
		}
	}
}
