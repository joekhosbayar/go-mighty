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
