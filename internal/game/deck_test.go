package game

import (
	"testing"
)

// TestNewDeck tests deck creation
// Rule: A standard deck has 52 cards plus 1 joker (53 total)
func TestNewDeck(t *testing.T) {
	deck := NewDeck()

	if deck == nil {
		t.Fatal("NewDeck() returned nil")
	}

	// Should have 53 cards total
	if len(deck.Cards) != 53 {
		t.Errorf("NewDeck() has %d cards, want 53", len(deck.Cards))
	}

	// Count cards by suit
	suitCounts := make(map[Suit]int)
	jokerCount := 0

	for _, card := range deck.Cards {
		if card.IsJoker() {
			jokerCount++
		} else {
			suitCounts[card.Suit]++
		}
	}

	// Should have 13 cards per suit
	for _, suit := range []Suit{Spades, Hearts, Diamonds, Clubs} {
		if suitCounts[suit] != 13 {
			t.Errorf("Deck has %d cards of suit %v, want 13", suitCounts[suit], suit)
		}
	}

	// Should have exactly 1 Joker
	if jokerCount != 1 {
		t.Errorf("Deck has %d jokers, want 1", jokerCount)
	}
}

// TestNewDeck_AllRanksPresent tests that all ranks are present in the deck
// Rule: Each suit should have all ranks from 2 to Ace
func TestNewDeck_AllRanksPresent(t *testing.T) {
	deck := NewDeck()

	expectedRanks := []Rank{Two, Three, Four, Five, Six, Seven, Eight, Nine, Ten, Jack, Queen, King, Ace}

	for _, suit := range []Suit{Spades, Hearts, Diamonds, Clubs} {
		for _, rank := range expectedRanks {
			found := false
			for _, card := range deck.Cards {
				if card.Suit == suit && card.Rank == rank {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Deck missing card: %v of %v", rank, suit)
			}
		}
	}
}

// TestDeck_Shuffle tests that shuffle randomizes card order
// Rule: After shuffling, cards should be in a different order
func TestDeck_Shuffle(t *testing.T) {
	deck := NewDeck()

	// Store original order
	originalOrder := make([]Card, len(deck.Cards))
	copy(originalOrder, deck.Cards)

	// Shuffle
	deck.Shuffle()

	// Check that order changed (statistically almost certain with 53 cards)
	sameCount := 0
	for i, card := range deck.Cards {
		if card.Equals(originalOrder[i]) {
			sameCount++
		}
	}

	// It's extremely unlikely all 53 cards remain in the same position
	if sameCount == 53 {
		t.Error("Shuffle() did not change card order")
	}

	// But all cards should still be present
	if len(deck.Cards) != 53 {
		t.Errorf("After shuffle, deck has %d cards, want 53", len(deck.Cards))
	}
}

// TestDeck_Shuffle_PreservesCards tests that shuffle doesn't lose or duplicate cards
// Rule: Shuffling should preserve the exact set of cards
func TestDeck_Shuffle_PreservesCards(t *testing.T) {
	deck := NewDeck()

	// Count cards before shuffle
	beforeCounts := make(map[string]int)
	for _, card := range deck.Cards {
		beforeCounts[card.String()]++
	}

	deck.Shuffle()

	// Count cards after shuffle
	afterCounts := make(map[string]int)
	for _, card := range deck.Cards {
		afterCounts[card.String()]++
	}

	// Compare counts
	if len(beforeCounts) != len(afterCounts) {
		t.Error("Shuffle changed the set of cards")
	}

	for cardStr, beforeCount := range beforeCounts {
		afterCount := afterCounts[cardStr]
		if beforeCount != afterCount {
			t.Errorf("Card %v count changed from %d to %d after shuffle", cardStr, beforeCount, afterCount)
		}
	}
}

// TestDeck_Deal_FivePlayers tests dealing to 5 players
// Rule: For 5 players, each gets 10 cards, 3 cards to kitty (53 total)
func TestDeck_Deal_FivePlayers(t *testing.T) {
	deck := NewDeck()
	deck.Shuffle()

	playerHands, kitty, err := deck.Deal(5)

	if err != nil {
		t.Fatalf("Deal(5) returned error: %v", err)
	}

	// Should have 5 hands
	if len(playerHands) != 5 {
		t.Errorf("Deal(5) returned %d hands, want 5", len(playerHands))
	}

	// Each hand should have 10 cards
	for i, hand := range playerHands {
		if len(hand) != 10 {
			t.Errorf("Player %d has %d cards, want 10", i, len(hand))
		}
	}

	// Kitty should have 3 cards
	if len(kitty) != 3 {
		t.Errorf("Kitty has %d cards, want 3", len(kitty))
	}

	// Total cards should be 53
	totalCards := len(kitty)
	for _, hand := range playerHands {
		totalCards += len(hand)
	}
	if totalCards != 53 {
		t.Errorf("Total cards dealt = %d, want 53", totalCards)
	}
}

// TestDeck_Deal_NoDuplicates tests that no card is dealt twice
// Rule: Each card should appear exactly once across all hands and kitty
func TestDeck_Deal_NoDuplicates(t *testing.T) {
	deck := NewDeck()
	deck.Shuffle()

	playerHands, kitty, err := deck.Deal(5)
	if err != nil {
		t.Fatalf("Deal(5) returned error: %v", err)
	}

	// Collect all dealt cards
	allCards := make([]Card, 0, 53)
	for _, hand := range playerHands {
		allCards = append(allCards, hand...)
	}
	allCards = append(allCards, kitty...)

	// Check for duplicates
	cardCounts := make(map[string]int)
	for _, card := range allCards {
		cardCounts[card.String()]++
	}

	for cardStr, count := range cardCounts {
		if count > 1 {
			t.Errorf("Card %v appears %d times, want 1", cardStr, count)
		}
	}
}

// TestDeck_Deal_AllCardsDealt tests that all cards from deck are dealt
// Rule: Every card in the original deck should be dealt
func TestDeck_Deal_AllCardsDealt(t *testing.T) {
	deck := NewDeck()

	// Remember original cards
	originalCards := make(map[string]bool)
	for _, card := range deck.Cards {
		originalCards[card.String()] = true
	}

	deck.Shuffle()
	playerHands, kitty, err := deck.Deal(5)
	if err != nil {
		t.Fatalf("Deal(5) returned error: %v", err)
	}

	// Collect all dealt cards
	dealtCards := make(map[string]bool)
	for _, hand := range playerHands {
		for _, card := range hand {
			dealtCards[card.String()] = true
		}
	}
	for _, card := range kitty {
		dealtCards[card.String()] = true
	}

	// Check all original cards were dealt
	for cardStr := range originalCards {
		if !dealtCards[cardStr] {
			t.Errorf("Card %v was not dealt", cardStr)
		}
	}

	// Check no extra cards were dealt
	if len(dealtCards) != len(originalCards) {
		t.Errorf("Dealt %d unique cards, want %d", len(dealtCards), len(originalCards))
	}
}

// TestDeck_Deal_InsufficientCards tests error when not enough cards
// Rule: Should return error if deck doesn't have enough cards
func TestDeck_Deal_InsufficientCards(t *testing.T) {
	deck := &Deck{
		Cards: make([]Card, 10), // Only 10 cards, not enough for 5 players
	}

	_, _, err := deck.Deal(5)

	if err != ErrInsufficientCards {
		t.Errorf("Deal(5) with 10 cards error = %v, want %v", err, ErrInsufficientCards)
	}
}

// TestDeck_Deal_EmptyDeck tests error when deck is empty
// Rule: Should return error if deck has no cards
func TestDeck_Deal_EmptyDeck(t *testing.T) {
	deck := &Deck{
		Cards: make([]Card, 0),
	}

	_, _, err := deck.Deal(5)

	if err != ErrInsufficientCards {
		t.Errorf("Deal(5) with empty deck error = %v, want %v", err, ErrInsufficientCards)
	}
}

// TestDeck_Deal_ThreePlayers tests dealing to 3 players
// Rule: Should be able to deal to different numbers of players (for game variants)
func TestDeck_Deal_ThreePlayers(t *testing.T) {
	deck := NewDeck()
	deck.Shuffle()

	playerHands, kitty, err := deck.Deal(3)

	if err != nil {
		t.Fatalf("Deal(3) returned error: %v", err)
	}

	// Should have 3 hands
	if len(playerHands) != 3 {
		t.Errorf("Deal(3) returned %d hands, want 3", len(playerHands))
	}

	// Each hand should have 10 cards
	for i, hand := range playerHands {
		if len(hand) != 10 {
			t.Errorf("Player %d has %d cards, want 10", i, len(hand))
		}
	}

	// Kitty should have 3 cards
	if len(kitty) != 3 {
		t.Errorf("Kitty has %d cards, want 3", len(kitty))
	}
}

// TestDeck_Remaining tests the Remaining() method
// Rule: Should accurately report number of cards left in deck
func TestDeck_Remaining(t *testing.T) {
	tests := []struct {
		name     string
		numCards int
	}{
		{
			name:     "Full deck should have 53 cards",
			numCards: 53,
		},
		{
			name:     "Empty deck should have 0 cards",
			numCards: 0,
		},
		{
			name:     "Partial deck should report correct count",
			numCards: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deck := &Deck{
				Cards: make([]Card, tt.numCards),
			}

			result := deck.Remaining()
			if result != tt.numCards {
				t.Errorf("Deck.Remaining() = %d, want %d", result, tt.numCards)
			}
		})
	}
}

// TestDeck_MultipleDealsSameResults tests that dealing pattern is consistent
// Rule: The dealing pattern (1-2-3-4 cards per round) should be consistent
func TestDeck_MultipleDealsSameResults(t *testing.T) {
	// Create two identical decks (unshuffled for predictability)
	deck1 := NewDeck()
	deck2 := NewDeck()

	playerHands1, kitty1, err1 := deck1.Deal(5)
	playerHands2, kitty2, err2 := deck2.Deal(5)

	if err1 != nil || err2 != nil {
		t.Fatalf("Deal errors: err1=%v, err2=%v", err1, err2)
	}

	// Compare hands
	for i := 0; i < 5; i++ {
		if len(playerHands1[i]) != len(playerHands2[i]) {
			t.Errorf("Player %d hand length differs: %d vs %d", i, len(playerHands1[i]), len(playerHands2[i]))
		}

		for j := 0; j < len(playerHands1[i]); j++ {
			if !playerHands1[i][j].Equals(playerHands2[i][j]) {
				t.Errorf("Player %d card %d differs: %v vs %v", i, j, playerHands1[i][j], playerHands2[i][j])
			}
		}
	}

	// Compare kitty
	if len(kitty1) != len(kitty2) {
		t.Errorf("Kitty length differs: %d vs %d", len(kitty1), len(kitty2))
	}

	for i := 0; i < len(kitty1); i++ {
		if !kitty1[i].Equals(kitty2[i]) {
			t.Errorf("Kitty card %d differs: %v vs %v", i, kitty1[i], kitty2[i])
		}
	}
}

// TestDeck_DealPattern tests the 1-2-3-4 dealing pattern
// Rule: Typical dealing pattern is 1 → 2 → 3 → 4 cards per player per round
func TestDeck_DealPattern(t *testing.T) {
	deck := NewDeck()

	playerHands, _, err := deck.Deal(5)
	if err != nil {
		t.Fatalf("Deal(5) returned error: %v", err)
	}

	// Pattern should be: 1+2+3+4 = 10 cards per player
	// This is tested implicitly by checking each player has 10 cards
	for i, hand := range playerHands {
		if len(hand) != 10 {
			t.Errorf("Player %d has %d cards, want 10 (pattern: 1+2+3+4)", i, len(hand))
		}
	}

	// The actual pattern can be verified by tracking card indices,
	// but the main requirement is that it totals to 10 per player
}

// TestDeck_Deal_PreservesPointValue tests that total point value is preserved
// Rule: Total point value across all dealt cards should be 20
func TestDeck_Deal_PreservesPointValue(t *testing.T) {
	deck := NewDeck()
	deck.Shuffle()

	playerHands, kitty, err := deck.Deal(5)
	if err != nil {
		t.Fatalf("Deal(5) returned error: %v", err)
	}

	totalPoints := 0

	// Count points in all hands
	for _, hand := range playerHands {
		for _, card := range hand {
			totalPoints += card.PointValue()
		}
	}

	// Count points in kitty
	for _, card := range kitty {
		totalPoints += card.PointValue()
	}

	if totalPoints != 20 {
		t.Errorf("Total points in dealt cards = %d, want 20", totalPoints)
	}
}
