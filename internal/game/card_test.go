package game

import (
	"testing"
)

// TestCard_String tests the string representation of cards
// Rule: Cards should be formatted as "SA", "H10", "JOKER", etc.
func TestCard_String(t *testing.T) {
	tests := []struct {
		name     string
		card     Card
		expected string
	}{
		{
			name:     "Spade Ace should format as 'SA'",
			card:     Card{Suit: Spades, Rank: Ace},
			expected: "SA",
		},
		{
			name:     "Heart Ten should format as 'H10'",
			card:     Card{Suit: Hearts, Rank: Ten},
			expected: "H10",
		},
		{
			name:     "Club Three should format as 'C3'",
			card:     Card{Suit: Clubs, Rank: Three},
			expected: "C3",
		},
		{
			name:     "Joker should format as 'JOKER'",
			card:     Card{Suit: NoSuit, Rank: Joker},
			expected: "JOKER",
		},
		{
			name:     "Diamond King should format as 'DK'",
			card:     Card{Suit: Diamonds, Rank: King},
			expected: "DK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.card.String()
			if result != tt.expected {
				t.Errorf("Card.String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestParseCard tests parsing card strings into Card objects
// Rule: Should correctly parse standard card notation and handle errors
func TestParseCard(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  Card
		shouldErr bool
	}{
		{
			name:      "Parse 'SA' should return Spade Ace",
			input:     "SA",
			expected:  Card{Suit: Spades, Rank: Ace},
			shouldErr: false,
		},
		{
			name:      "Parse 'H10' should return Heart Ten",
			input:     "H10",
			expected:  Card{Suit: Hearts, Rank: Ten},
			shouldErr: false,
		},
		{
			name:      "Parse 'JOKER' should return Joker",
			input:     "JOKER",
			expected:  Card{Suit: NoSuit, Rank: Joker},
			shouldErr: false,
		},
		{
			name:      "Parse lowercase 'sa' should return Spade Ace",
			input:     "sa",
			expected:  Card{Suit: Spades, Rank: Ace},
			shouldErr: false,
		},
		{
			name:      "Parse 'C3' should return Club Three",
			input:     "C3",
			expected:  Card{Suit: Clubs, Rank: Three},
			shouldErr: false,
		},
		{
			name:      "Parse invalid single character should error",
			input:     "X",
			shouldErr: true,
		},
		{
			name:      "Parse invalid suit should error",
			input:     "XA",
			shouldErr: true,
		},
		{
			name:      "Parse empty string should error",
			input:     "",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseCard(tt.input)
			if tt.shouldErr {
				if err == nil {
					t.Errorf("ParseCard(%v) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("ParseCard(%v) unexpected error: %v", tt.input, err)
				}
				if !result.Equals(tt.expected) {
					t.Errorf("ParseCard(%v) = %v, want %v", tt.input, result, tt.expected)
				}
			}
		})
	}
}

// TestCard_PointValue tests point values of cards
// Rule: A, K, Q, J, 10 are worth 1 point each (20 total points in deck)
func TestCard_PointValue(t *testing.T) {
	tests := []struct {
		name     string
		card     Card
		expected int
	}{
		{
			name:     "Ace should be worth 1 point",
			card:     Card{Suit: Spades, Rank: Ace},
			expected: 1,
		},
		{
			name:     "King should be worth 1 point",
			card:     Card{Suit: Hearts, Rank: King},
			expected: 1,
		},
		{
			name:     "Queen should be worth 1 point",
			card:     Card{Suit: Diamonds, Rank: Queen},
			expected: 1,
		},
		{
			name:     "Jack should be worth 1 point",
			card:     Card{Suit: Clubs, Rank: Jack},
			expected: 1,
		},
		{
			name:     "Ten should be worth 1 point",
			card:     Card{Suit: Spades, Rank: Ten},
			expected: 1,
		},
		{
			name:     "Nine should be worth 0 points",
			card:     Card{Suit: Hearts, Rank: Nine},
			expected: 0,
		},
		{
			name:     "Two should be worth 0 points",
			card:     Card{Suit: Clubs, Rank: Two},
			expected: 0,
		},
		{
			name:     "Joker should be worth 0 points",
			card:     Card{Suit: NoSuit, Rank: Joker},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.card.PointValue()
			if result != tt.expected {
				t.Errorf("Card.PointValue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestCard_IsMighty tests Mighty card identification
// Rule: Mighty is SA unless spades are trump, then DA
func TestCard_IsMighty(t *testing.T) {
	tests := []struct {
		name     string
		card     Card
		trump    Suit
		expected bool
	}{
		{
			name:     "SA is Mighty when trump is Hearts",
			card:     Card{Suit: Spades, Rank: Ace},
			trump:    Hearts,
			expected: true,
		},
		{
			name:     "SA is Mighty when trump is Diamonds",
			card:     Card{Suit: Spades, Rank: Ace},
			trump:    Diamonds,
			expected: true,
		},
		{
			name:     "SA is Mighty when trump is Clubs",
			card:     Card{Suit: Spades, Rank: Ace},
			trump:    Clubs,
			expected: true,
		},
		{
			name:     "SA is NOT Mighty when trump is Spades",
			card:     Card{Suit: Spades, Rank: Ace},
			trump:    Spades,
			expected: false,
		},
		{
			name:     "DA is Mighty when trump is Spades",
			card:     Card{Suit: Diamonds, Rank: Ace},
			trump:    Spades,
			expected: true,
		},
		{
			name:     "DA is NOT Mighty when trump is Hearts",
			card:     Card{Suit: Diamonds, Rank: Ace},
			trump:    Hearts,
			expected: false,
		},
		{
			name:     "SK is NOT Mighty regardless of trump",
			card:     Card{Suit: Spades, Rank: King},
			trump:    Hearts,
			expected: false,
		},
		{
			name:     "Joker is NOT Mighty",
			card:     Card{Suit: NoSuit, Rank: Joker},
			trump:    Hearts,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.card.IsMighty(tt.trump)
			if result != tt.expected {
				t.Errorf("Card.IsMighty(%v) = %v, want %v", tt.trump, result, tt.expected)
			}
		})
	}
}

// TestCard_IsJoker tests Joker identification
// Rule: Only the Joker card should return true
func TestCard_IsJoker(t *testing.T) {
	tests := []struct {
		name     string
		card     Card
		expected bool
	}{
		{
			name:     "Joker should be identified as Joker",
			card:     Card{Suit: NoSuit, Rank: Joker},
			expected: true,
		},
		{
			name:     "Spade Ace should NOT be Joker",
			card:     Card{Suit: Spades, Rank: Ace},
			expected: false,
		},
		{
			name:     "Any regular card should NOT be Joker",
			card:     Card{Suit: Hearts, Rank: Ten},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.card.IsJoker()
			if result != tt.expected {
				t.Errorf("Card.IsJoker() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestCard_IsRipper tests Ripper (Joker Hunter) identification
// Rule: Ripper is C3 unless clubs are trump, then S3
func TestCard_IsRipper(t *testing.T) {
	tests := []struct {
		name     string
		card     Card
		trump    Suit
		expected bool
	}{
		{
			name:     "C3 is Ripper when trump is Hearts",
			card:     Card{Suit: Clubs, Rank: Three},
			trump:    Hearts,
			expected: true,
		},
		{
			name:     "C3 is Ripper when trump is Spades",
			card:     Card{Suit: Clubs, Rank: Three},
			trump:    Spades,
			expected: true,
		},
		{
			name:     "C3 is Ripper when trump is Diamonds",
			card:     Card{Suit: Clubs, Rank: Three},
			trump:    Diamonds,
			expected: true,
		},
		{
			name:     "C3 is NOT Ripper when trump is Clubs",
			card:     Card{Suit: Clubs, Rank: Three},
			trump:    Clubs,
			expected: false,
		},
		{
			name:     "S3 is Ripper when trump is Clubs",
			card:     Card{Suit: Spades, Rank: Three},
			trump:    Clubs,
			expected: true,
		},
		{
			name:     "S3 is NOT Ripper when trump is Spades",
			card:     Card{Suit: Spades, Rank: Three},
			trump:    Spades,
			expected: false,
		},
		{
			name:     "C4 is NOT Ripper",
			card:     Card{Suit: Clubs, Rank: Four},
			trump:    Hearts,
			expected: false,
		},
		{
			name:     "Joker is NOT Ripper",
			card:     Card{Suit: NoSuit, Rank: Joker},
			trump:    Hearts,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.card.IsRipper(tt.trump)
			if result != tt.expected {
				t.Errorf("Card.IsRipper(%v) = %v, want %v", tt.trump, result, tt.expected)
			}
		})
	}
}

// TestCard_IsMagicCard tests Magic Card identification
// Rule: Magic Cards are Mighty and Joker together
func TestCard_IsMagicCard(t *testing.T) {
	tests := []struct {
		name     string
		card     Card
		trump    Suit
		expected bool
	}{
		{
			name:     "SA is Magic Card when not trump",
			card:     Card{Suit: Spades, Rank: Ace},
			trump:    Hearts,
			expected: true,
		},
		{
			name:     "DA is Magic Card when Spades are trump",
			card:     Card{Suit: Diamonds, Rank: Ace},
			trump:    Spades,
			expected: true,
		},
		{
			name:     "Joker is Magic Card",
			card:     Card{Suit: NoSuit, Rank: Joker},
			trump:    Hearts,
			expected: true,
		},
		{
			name:     "SK is NOT Magic Card",
			card:     Card{Suit: Spades, Rank: King},
			trump:    Hearts,
			expected: false,
		},
		{
			name:     "Regular card is NOT Magic Card",
			card:     Card{Suit: Hearts, Rank: Ten},
			trump:    Hearts,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.card.IsMagicCard(tt.trump)
			if result != tt.expected {
				t.Errorf("Card.IsMagicCard(%v) = %v, want %v", tt.trump, result, tt.expected)
			}
		})
	}
}

// TestCard_RankValue tests numeric rank values for comparison
// Rule: Higher ranks should have higher values (Ace=14, King=13, ..., Two=2)
func TestCard_RankValue(t *testing.T) {
	tests := []struct {
		name     string
		card     Card
		expected int
	}{
		{
			name:     "Joker should have highest value (15)",
			card:     Card{Suit: NoSuit, Rank: Joker},
			expected: 15,
		},
		{
			name:     "Ace should have value 14",
			card:     Card{Suit: Spades, Rank: Ace},
			expected: 14,
		},
		{
			name:     "King should have value 13",
			card:     Card{Suit: Hearts, Rank: King},
			expected: 13,
		},
		{
			name:     "Queen should have value 12",
			card:     Card{Suit: Diamonds, Rank: Queen},
			expected: 12,
		},
		{
			name:     "Jack should have value 11",
			card:     Card{Suit: Clubs, Rank: Jack},
			expected: 11,
		},
		{
			name:     "Ten should have value 10",
			card:     Card{Suit: Spades, Rank: Ten},
			expected: 10,
		},
		{
			name:     "Nine should have value 9",
			card:     Card{Suit: Hearts, Rank: Nine},
			expected: 9,
		},
		{
			name:     "Two should have value 2",
			card:     Card{Suit: Clubs, Rank: Two},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.card.RankValue()
			if result != tt.expected {
				t.Errorf("Card.RankValue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestCard_RankValueOrdering tests that rank values maintain proper ordering
// Rule: Ace > King > Queen > Jack > 10 > 9 > ... > 2
func TestCard_RankValueOrdering(t *testing.T) {
	ace := Card{Suit: Spades, Rank: Ace}
	king := Card{Suit: Spades, Rank: King}
	queen := Card{Suit: Spades, Rank: Queen}
	jack := Card{Suit: Spades, Rank: Jack}
	ten := Card{Suit: Spades, Rank: Ten}
	two := Card{Suit: Spades, Rank: Two}

	if ace.RankValue() <= king.RankValue() {
		t.Error("Ace should have higher value than King")
	}
	if king.RankValue() <= queen.RankValue() {
		t.Error("King should have higher value than Queen")
	}
	if queen.RankValue() <= jack.RankValue() {
		t.Error("Queen should have higher value than Jack")
	}
	if jack.RankValue() <= ten.RankValue() {
		t.Error("Jack should have higher value than Ten")
	}
	if ten.RankValue() <= two.RankValue() {
		t.Error("Ten should have higher value than Two")
	}
}

// TestCard_Equals tests card equality comparison
// Rule: Two cards are equal if they have the same suit and rank
func TestCard_Equals(t *testing.T) {
	tests := []struct {
		name     string
		card1    Card
		card2    Card
		expected bool
	}{
		{
			name:     "Same card should be equal",
			card1:    Card{Suit: Spades, Rank: Ace},
			card2:    Card{Suit: Spades, Rank: Ace},
			expected: true,
		},
		{
			name:     "Different suits should not be equal",
			card1:    Card{Suit: Spades, Rank: Ace},
			card2:    Card{Suit: Hearts, Rank: Ace},
			expected: false,
		},
		{
			name:     "Different ranks should not be equal",
			card1:    Card{Suit: Spades, Rank: Ace},
			card2:    Card{Suit: Spades, Rank: King},
			expected: false,
		},
		{
			name:     "Completely different cards should not be equal",
			card1:    Card{Suit: Spades, Rank: Ace},
			card2:    Card{Suit: Hearts, Rank: Two},
			expected: false,
		},
		{
			name:     "Two Jokers should be equal",
			card1:    Card{Suit: NoSuit, Rank: Joker},
			card2:    Card{Suit: NoSuit, Rank: Joker},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.card1.Equals(tt.card2)
			if result != tt.expected {
				t.Errorf("Card.Equals() = %v, want %v for %v vs %v", result, tt.expected, tt.card1, tt.card2)
			}
		})
	}
}

// TestCard_StringAndParseRoundTrip tests that String() and ParseCard() are inverse operations
// Rule: Parsing the string representation of a card should yield the original card
func TestCard_StringAndParseRoundTrip(t *testing.T) {
	cards := []Card{
		{Suit: Spades, Rank: Ace},
		{Suit: Hearts, Rank: Ten},
		{Suit: Diamonds, Rank: King},
		{Suit: Clubs, Rank: Three},
		{Suit: NoSuit, Rank: Joker},
	}

	for _, original := range cards {
		t.Run("RoundTrip_"+original.String(), func(t *testing.T) {
			str := original.String()
			parsed, err := ParseCard(str)
			if err != nil {
				t.Errorf("ParseCard(%v) returned error: %v", str, err)
			}
			if !parsed.Equals(original) {
				t.Errorf("Round trip failed: original=%v, string=%v, parsed=%v", original, str, parsed)
			}
		})
	}
}
