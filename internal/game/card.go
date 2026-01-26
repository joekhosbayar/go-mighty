package game

import (
	"fmt"
	"math/rand"
)

// Suit represents the card suit
type Suit string

const (
	Spades   Suit = "spades"
	Diamonds Suit = "diamonds"
	Hearts   Suit = "hearts"
	Clubs    Suit = "clubs"
	None     Suit = "none" // For Joker
)

// Rank represents the card rank
type Rank string

const (
	Ace   Rank = "A"
	King  Rank = "K"
	Queen Rank = "Q"
	Jack  Rank = "J"
	Ten   Rank = "10"
	Nine  Rank = "9"
	Eight Rank = "8"
	Seven Rank = "7"
	Six   Rank = "6"
	Five  Rank = "5"
	Four  Rank = "4"
	Three Rank = "3"
	Two   Rank = "2"
	// In Mighty, there is typically one Joker. We will use "Joker" as the only Joker rank.
	Joker Rank = "Joker"
)

// Card represents a playing card
type Card struct {
	Suit Suit `json:"suit"`
	Rank Rank `json:"rank"`
}

func (c Card) String() string {
	if c.Rank == Joker {
		return "Joker"
	}
	s := string(c.Suit)
	prefix := ""
	if len(s) > 0 {
		prefix = s[:1]
	}
	return fmt.Sprintf("%s%s", prefix, c.Rank) // e.g. S-A -> SA
}

// IsPointCard checks if the card is a point card (A, K, Q, J, 10)
func (c Card) IsPointCard() bool {
	switch c.Rank {
	case Ace, King, Queen, Jack, Ten:
		return true
	}
	return false
}

// Deck represents a deck of cards
type Deck []Card

// NewDeck creates a standard 53-card deck (52 + 1 Joker)
func NewDeck() Deck {
	suits := []Suit{Spades, Diamonds, Hearts, Clubs}
	ranks := []Rank{Ace, King, Queen, Jack, Ten, Nine, Eight, Seven, Six, Five, Four, Three, Two}

	deck := make(Deck, 0, 53)
	for _, s := range suits {
		for _, r := range ranks {
			deck = append(deck, Card{Suit: s, Rank: r})
		}
	}
	deck = append(deck, Card{Suit: None, Rank: Joker})
	return deck
}

// Shuffle shuffles the deck
func (d Deck) Shuffle() {
	rand.Shuffle(len(d), func(i, j int) {
		d[i], d[j] = d[j], d[i]
	})
}

// Deal distributed cards to 5 players (10 each) and kitty (3)
// Returns 5 hands and the kitty
func (d Deck) Deal() ([5][]Card, []Card) {
	if len(d) != 53 {
		// Should unlikely happen if fresh deck
		return [5][]Card{}, nil
	}

	hands := [5][]Card{}
	// Mighty dealing: 1 -> 2 -> 3 -> 4 is common, or just purely random.
	// We'll just deal sequentially for simplicity as shuffle is random.

	// Implementation:
	// Players 0-4 get 10 cards each. Top 50 cards used.
	// Last 3 cards go to kitty.

	// Actually, let's just slice it.
	k := 0
	for i := 0; i < 5; i++ {
		hands[i] = make([]Card, 10)
		for j := 0; j < 10; j++ {
			hands[i][j] = d[k]
			k++
		}
	}
	kitty := make([]Card, 3)
	copy(kitty, d[50:])

	return hands, kitty
}
