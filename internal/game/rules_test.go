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
	// Simulate middle trick
	g.Tricks = append(g.Tricks, Trick{}, Trick{}) 
	trick := Trick{LeadSuit: Clubs}

	mighty := Card{Suit: Spades, Rank: Ace}
	joker := Card{Suit: None, Rank: Joker}
	trumpK := Card{Suit: Hearts, Rank: King}
	clubA := Card{Suit: Clubs, Rank: Ace}
	clubK := Card{Suit: Clubs, Rank: King}
	diamondK := Card{Suit: Diamonds, Rank: King}

	// 1. Mighty beats Joker
	if !g.Beats(mighty, joker, trick) {
		t.Errorf("Mighty should beat Joker")
	}

	// 2. Joker beats Trump K
	if !g.Beats(joker, trumpK, trick) {
		t.Errorf("Joker should beat Trump K")
	}

	// 3. Trump K beats Lead Suit A (Club A)
	if !g.Beats(trumpK, clubA, trick) {
		t.Errorf("Trump should beat Lead Suit A")
	}

	// 4. Lead Suit A beats Lead Suit K
	if !g.Beats(clubA, clubK, trick) {
		t.Errorf("Higher rank should win in lead suit")
	}

	// 5. Lead Suit A beats Off Suit K (Diamond King) - Non Trump
	if !g.Beats(clubA, diamondK, trick) {
		t.Errorf("Lead suit should beat off suit")
	}
}

func TestJokerExceptions(t *testing.T) {
	g := NewGame("test-joker")
	g.Trump = Hearts
	joker := Card{Suit: None, Rank: Joker}
	clubA := Card{Suit: Clubs, Rank: Ace}

	// Trick 1: Joker has 0 power
	g.Tricks = []Trick{{}} // One trick active (Trick 1)
	t1 := Trick{LeadSuit: Clubs}
	if g.Beats(joker, clubA, t1) {
		t.Errorf("Joker should lose on trick 1")
	}

	// Trick 10: Joker has 0 power
	g.Tricks = make([]Trick, 10)
	t10 := Trick{LeadSuit: Clubs}
	if g.Beats(joker, clubA, t10) {
		t.Errorf("Joker should lose on trick 10")
	}

	// Joker Called: Joker has 0 power
	g.Tricks = make([]Trick, 5)
	tCalled := Trick{LeadSuit: Clubs, JokerCalled: true}
	if g.Beats(joker, clubA, tCalled) {
		t.Errorf("Joker should lose when called")
	}
}

func TestJokerCaller(t *testing.T) {
	g := NewGame("test-caller")
	g.Trump = Hearts

	// Standard: Clubs 3 is Joker Caller
	caller := Card{Suit: Clubs, Rank: Three}
	if !g.IsJokerCaller(caller) {
		t.Errorf("Clubs 3 should be Joker Caller")
	}

	// Clubs Trump: Spades 3 is Joker Caller
	g.Trump = Clubs
	spades3 := Card{Suit: Spades, Rank: Three}
	if !g.IsJokerCaller(spades3) {
		t.Errorf("Spades 3 should be Joker Caller when Clubs is Trump")
	}
}

func TestMightyIdentity(t *testing.T) {
	g := NewGame("test-mighty")
	
	// Hearts Trump: Spades Ace is Mighty
	g.Trump = Hearts
	if !g.IsMighty(Card{Suit: Spades, Rank: Ace}) {
		t.Errorf("Spades Ace should be Mighty when Hearts is Trump")
	}

	// Spades Trump: Clubs Ace is Mighty
	g.Trump = Spades
	if !g.IsMighty(Card{Suit: Clubs, Rank: Ace}) {
		t.Errorf("Clubs Ace should be Mighty when Spades is Trump")
	}
}

func TestFirstTrickTrumpLead(t *testing.T) {
	g := NewGame("test-first-lead")
	g.Players[0] = &Player{ID: "P1", Seat: 0, Hand: []Card{
		{Suit: Hearts, Rank: Ace},   // Trump
		{Suit: Clubs, Rank: Two},    // Non-trump
	}}
	g.Status = PhasePlaying
	g.Trump = Hearts
	g.Tricks = []Trick{{}} // Trick 1

	// Leading trump on trick 1 with non-trump in hand -> Error
	move := PlayCardMove{Card: Card{Suit: Hearts, Rank: Ace}}
	err := g.ValidateMove("P1", MovePlayCard, move)
	if err == nil {
		t.Errorf("Should reject trump lead on trick 1")
	}

	// Leading non-trump -> OK
	move2 := PlayCardMove{Card: Card{Suit: Clubs, Rank: Two}}
	err = g.ValidateMove("P1", MovePlayCard, move2)
	if err != nil {
		t.Errorf("Should accept non-trump lead on trick 1: %v", err)
	}
}

func TestJokerCallerForce(t *testing.T) {
	g := NewGame("test-force")
	g.Players[1] = &Player{ID: "P2", Seat: 1, Hand: []Card{
		{Suit: None, Rank: Joker},
		{Suit: Hearts, Rank: Two},
	}}
	g.Status = PhasePlaying
	g.CurrentTurn = 1
	g.Tricks = []Trick{{
		Cards: []PlayedCard{
			{PlayerID: "P1", Seat: 0, Card: Card{Suit: Clubs, Rank: Three}},
		},
		LeadSuit:    Clubs,
		JokerCalled: true,
	}}

	// Must play Joker
	move := PlayCardMove{Card: Card{Suit: Hearts, Rank: Two}}
	err := g.ValidateMove("P2", MovePlayCard, move)
	if err == nil {
		t.Errorf("Should force Joker play")
	}

	move2 := PlayCardMove{Card: Card{Suit: None, Rank: Joker}}
	err = g.ValidateMove("P2", MovePlayCard, move2)
	if err != nil {
		t.Errorf("Should allow forced Joker play: %v", err)
	}
}

func TestScoring(t *testing.T) {
	g := NewGame("test-scoring")
	g.Declarer = 0
	g.PartnerSeat = 1
	g.Contract = &Bid{Points: 7, Suit: Spades, IsNoTrump: false}
	
	// Simulate 8 tricks won by team (Declarer + Partner)
	for i := 0; i < 8; i++ {
		g.Tricks = append(g.Tricks, Trick{Winner: 0})
	}
	// 2 tricks won by opponents
	for i := 0; i < 2; i++ {
		g.Tricks = append(g.Tricks, Trick{Winner: 2})
	}

	// 7-spade bid, 8 tricks won -> 7*10 + 1*5 = 75
	score, friendScore := g.CalculateFinalScore()
	if score != 75 {
		t.Errorf("Expected score 75, got %v", score)
	}
	if friendScore != 37.5 {
		t.Errorf("Expected friend score 37.5, got %v", friendScore)
	}

	// Test No-Trump Multiplier
	g.Contract.IsNoTrump = true
	score, _ = g.CalculateFinalScore()
	if score != 150 { // 75 * 2
		t.Errorf("Expected No-Trump score 150, got %v", score)
	}

	// Test No-Friend Multiplier
	g.IsNoFriend = true
	g.PartnerSeat = -1
	score, friendScore = g.CalculateFinalScore()
	if score != 300 { // 150 * 2
		t.Errorf("Expected No-Friend score 300, got %v", score)
	}
	if friendScore != 0 {
		t.Errorf("Expected friend score 0 for No-Friend game")
	}

	// Test 10-bid Multiplier
	g.Contract.Points = 10
	// Recalculate tricks won (all 10 now for 10-bid)
	g.Tricks = nil
	for i := 0; i < 10; i++ {
		g.Tricks = append(g.Tricks, Trick{Winner: 0})
	}
	// 10-bid, 10 tricks, NT, NoFriend -> (10*10)*2*2*2 = 800
	score, _ = g.CalculateFinalScore()
	if score != 800 {
		t.Errorf("Expected ultimate score 800, got %v", score)
	}
}
