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
