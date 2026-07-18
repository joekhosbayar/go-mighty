package game

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestIsMighty(t *testing.T) {
	g := New("test")
	g.Trump = Spades
	
	assert.True(t, g.IsMighty(Card{Suit: Diamonds, Rank: Ace}), "Spades trump -> Mighty is Ace of Diamonds")
	assert.False(t, g.IsMighty(Card{Suit: Clubs, Rank: Ace}), "Spades trump -> Mighty is NOT Ace of Clubs")

	g.Trump = Hearts
	assert.True(t, g.IsMighty(Card{Suit: Spades, Rank: Ace}), "Hearts trump -> Mighty is Ace of Spades")
}

func TestPlayerHelpers(t *testing.T) {
	g := New("test")
	g.Trump = Hearts
	p := &Player{Hand: []Card{
		{Suit: Spades, Rank: Ace},   // Mighty
		{Suit: Hearts, Rank: Three}, // Trump
		{Suit: None, Rank: Joker},   // Joker
	}}

	assert.True(t, p.HasMighty(g))
	assert.False(t, p.HasNonTrumpMightyJoker(g), "Hand only has Trump, Mighty, Joker")
	assert.Equal(t, 1, p.GetSuitCount(Spades))

	p.Hand = append(p.Hand, Card{Suit: Clubs, Rank: Five})
	assert.True(t, p.HasNonTrumpMightyJoker(g))
	assert.Equal(t, 1, p.GetSuitCount(Clubs))
}

func TestLateGameSpecialForcing(t *testing.T) {
	g := New("test")
	g.Trump = Hearts
	g.Status = PhasePlaying
	p := &Player{ID: "p1", Hand: []Card{
		{Suit: Spades, Rank: Ace}, // Mighty
		{Suit: Clubs, Rank: Five},
	}}
	g.Players[0] = p
	g.CurrentTurn = 0
	g.Tricks = []Trick{{LeadSuit: Clubs}} // Active trick, 9th trick (since hand has 2 cards left)

	// Trying to follow suit with 2 cards left and holding Mighty MUST fail.
	err := g.ValidateMove("p1", MovePlayCard, PlayCardMove{Card: Card{Suit: Clubs, Rank: Five}})
	assert.ErrorContains(t, err, "must play mighty or joker")

	// Playing Mighty is allowed and forced.
	err = g.ValidateMove("p1", MovePlayCard, PlayCardMove{Card: Card{Suit: Spades, Rank: Ace}})
	assert.NoError(t, err)

	p.Hand = []Card{{Suit: Spades, Rank: Ace}, {Suit: None, Rank: Joker}, {Suit: Clubs, Rank: Five}}
	// 3 cards left, holding BOTH. Must play one.
	err = g.ValidateMove("p1", MovePlayCard, PlayCardMove{Card: Card{Suit: Clubs, Rank: Five}})
	assert.ErrorContains(t, err, "must play mighty or joker")
}
