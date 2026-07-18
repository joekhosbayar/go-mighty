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
	g.Tricks = make([]Trick, 9)
	g.Tricks[8] = Trick{LeadSuit: Clubs} // Active trick, 9th trick (since hand has 2 cards left)

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

func TestFirstTrickRestrictions(t *testing.T) {
	g := New("test")
	g.Trump = Hearts
	g.Status = PhasePlaying
	p1 := &Player{ID: "p1", Hand: []Card{{Suit: Hearts, Rank: Ace}, {Suit: Clubs, Rank: Five}}}
	g.Players[0] = p1
	g.CurrentTurn = 0
	
	// Opener leads Trick 1
	g.Tricks = append(g.Tricks, Trick{}) 
	
	// Opener tries to lead Trump
	err := g.ValidateMove("p1", MovePlayCard, PlayCardMove{Card: Card{Suit: Hearts, Rank: Ace}})
	assert.ErrorContains(t, err, "cannot lead trump on first trick")

	// Follower playing Mighty
	g.Tricks[0].LeadSuit = Spades
	g.Tricks[0].Cards = append(g.Tricks[0].Cards, PlayedCard{})
	
	p2 := &Player{ID: "p2", Hand: []Card{{Suit: Spades, Rank: Ace}, {Suit: Spades, Rank: Two}}}
	g.Players[1] = p2
	g.CurrentTurn = 1

	err = g.ValidateMove("p2", MovePlayCard, PlayCardMove{Card: Card{Suit: Spades, Rank: Ace}})
	assert.ErrorContains(t, err, "cannot play mighty on first trick unless it is your only card of the led suit")
}
