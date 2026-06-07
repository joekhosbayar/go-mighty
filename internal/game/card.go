// Package game implements the core logic of the Mighty card game, including
// card definitions, game state management, and rules enforcement.
package game

import (
	"fmt"
	"math/rand"
)

// Suit represents the card suit.
type Suit string

const (
	// Spades represents the spades suit.
	Spades Suit = "spades"
	// Diamonds represents the diamonds suit.
	Diamonds Suit = "diamonds"
	// Hearts represents the hearts suit.
	Hearts Suit = "hearts"
	// Clubs represents the clubs suit.
	Clubs Suit = "clubs"
	// None represents no suit (used for No-Trump or Joker).
	None Suit = "none" // For Joker
)

// Rank represents the card rank.
type Rank string

const (
	// Ace represents the Ace rank.
	Ace Rank = "A"
	// King represents the King rank.
	King Rank = "K"
	// Queen represents the Queen rank.
	Queen Rank = "Q"
	// Jack represents the Jack rank.
	Jack Rank = "J"
	// Ten represents the Ten rank.
	Ten Rank = "10"
	// Nine represents the Nine rank.
	Nine Rank = "9"
	// Eight represents the Eight rank.
	Eight Rank = "8"
	// Seven represents the Seven rank.
	Seven Rank = "7"
	// Six represents the Six rank.
	Six Rank = "6"
	// Five represents the Five rank.
	Five Rank = "5"
	// Four represents the Four rank.
	Four Rank = "4"
	// Three represents the Three rank.
	Three Rank = "3"
	// Two represents the Two rank.
	Two Rank = "2"
	// Joker represents the Joker rank.
	// In Mighty, there is typically one Joker. We will use "Joker" as the only Joker rank.
	Joker Rank = "Joker"
)

// Card represents a playing card.
type Card struct {
	Suit Suit `json:"suit"`
	Rank Rank `json:"rank"`
}

// String returns a string representation of the card.
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

// IsPointCard checks if the card is a point card (A, K, Q, J, 10).
func (c Card) IsPointCard() bool {
	switch c.Rank {
	case Ace, King, Queen, Jack, Ten:
		return true
	case Nine, Eight, Seven, Six, Five, Four, Three, Two, Joker:
		return false
	default:
		return false
	}
}

// Deck represents a deck of cards.
type Deck []Card

// NewDeck creates a standard 53-card deck (52 + 1 Joker).
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

// Shuffle shuffles the deck.
func (d Deck) Shuffle() {
	rand.Shuffle(len(d), func(i, j int) {
		d[i], d[j] = d[j], d[i]
	})
}

// Deal distributed cards to 5 players (10 each) and kitty (3)
// Returns 5 hands and the kitty.
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

	for i := range 5 {
		hands[i] = make([]Card, 10)
		for j := range 10 {
			hands[i][j] = d[k]
			k++
		}
	}

	kitty := make([]Card, 3)
	copy(kitty, d[50:])

	return hands, kitty
}
