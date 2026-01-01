package game

import (
	"math/rand"
	"time"
)

// Deck represents a deck of cards
type Deck struct {
	Cards []Card
}

// NewDeck creates a standard 52-card deck plus one joker (53 cards total)
func NewDeck() *Deck {
	deck := &Deck{
		Cards: make([]Card, 0, 53),
	}

	// Add all standard cards
	suits := []Suit{Spades, Hearts, Diamonds, Clubs}
	ranks := []Rank{Two, Three, Four, Five, Six, Seven, Eight, Nine, Ten, Jack, Queen, King, Ace}

	for _, suit := range suits {
		for _, rank := range ranks {
			deck.Cards = append(deck.Cards, Card{Suit: suit, Rank: rank})
		}
	}

	// Add Joker
	deck.Cards = append(deck.Cards, Card{Suit: NoSuit, Rank: Joker})

	return deck
}

// Shuffle randomizes the order of cards in the deck
func (d *Deck) Shuffle() {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(d.Cards), func(i, j int) {
		d.Cards[i], d.Cards[j] = d.Cards[j], d.Cards[i]
	})
}

// Deal deals cards to players and creates a kitty
// For 5 players: each gets 10 cards, 3 cards to kitty
func (d *Deck) Deal(numPlayers int) (playerHands [][]Card, kitty []Card, err error) {
	if len(d.Cards) < 53 {
		return nil, nil, ErrInsufficientCards
	}

	cardsPerPlayer := 10
	kittySize := 3

	if numPlayers*cardsPerPlayer+kittySize > len(d.Cards) {
		return nil, nil, ErrInsufficientCards
	}

	playerHands = make([][]Card, numPlayers)
	for i := 0; i < numPlayers; i++ {
		playerHands[i] = make([]Card, 0, cardsPerPlayer)
	}

	// Deal pattern: 1-2-3-4 cards per player per round
	pattern := []int{1, 2, 3, 4}
	cardIndex := 0

	for _, count := range pattern {
		for player := 0; player < numPlayers; player++ {
			for c := 0; c < count; c++ {
				if cardIndex >= len(d.Cards) {
					return nil, nil, ErrInsufficientCards
				}
				playerHands[player] = append(playerHands[player], d.Cards[cardIndex])
				cardIndex++
			}
		}
	}

	// Remaining 3 cards go to kitty
	kitty = d.Cards[cardIndex : cardIndex+kittySize]

	return playerHands, kitty, nil
}

// Remaining returns the number of cards left in the deck
func (d *Deck) Remaining() int {
	return len(d.Cards)
}
