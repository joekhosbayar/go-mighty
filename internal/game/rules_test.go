package game

import (
	"fmt"
	"testing"
)

func TestGameFlow(t *testing.T) {
	t.Parallel()
	// 1. Initialize Game
	g := New("test-game")

	// Add players
	for i := range 5 {
		p := &Player{ID: string(rune('A' + i)), Seat: i, Name: string(rune('A' + i))}
		g.Players[i] = p
	}

	// 2. Start (Deal)
	g.Start()

	if len(g.Players[0].Hand) != 10 {
		t.Errorf("Expected 10 cards, got %d", len(g.Players[0].Hand))
	}

	// 3. Bid
	// Player 0 (current turn) bids 7 S
	err := g.ValidateMove(g.Players[0].ID, MoveBid, Bid{Points: 7, Suit: Spades})
	if err != nil {
		t.Errorf("Valid bid rejected: %v", err)
	}

	_ = g.ApplyMove(g.Players[0].ID, MoveBid, Bid{Points: 7, Suit: Spades})

	if g.CurrentBid == nil || g.CurrentBid.Points != 7 {
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

	_ = g.ApplyMove(g.Players[1].ID, MovePass, nil)

	// Simulate others pass
	_ = g.ApplyMove(g.Players[2].ID, MovePass, nil)
	_ = g.ApplyMove(g.Players[3].ID, MovePass, nil)

	// Player 4 attempts a same-point bid; should be rejected
	err = g.ValidateMove(g.Players[4].ID, MoveBid, Bid{Points: 7, Suit: Spades})
	if err == nil {
		t.Errorf("Expected error for same-point bid")
	}

	_ = g.ApplyMove(g.Players[4].ID, MovePass, nil)

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
	t.Parallel()
	g := New("test")
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
	t.Parallel()
	g := New("test-joker")
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
	t.Parallel()
	g := New("test-caller")
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
	t.Parallel()
	g := New("test-mighty")

	// Hearts Trump: Spades Ace is Mighty
	g.Trump = Hearts
	if !g.IsMighty(Card{Suit: Spades, Rank: Ace}) {
		t.Errorf("Spades Ace should be Mighty when Hearts is Trump")
	}

	// Spades Trump: Diamonds Ace is Mighty
	g.Trump = Spades
	if !g.IsMighty(Card{Suit: Diamonds, Rank: Ace}) {
		t.Errorf("Diamonds Ace should be Mighty when Spades is Trump")
	}
}

func TestFirstTrickTrumpLead(t *testing.T) {
	t.Parallel()
	g := New("test-first-lead")
	g.Players[0] = &Player{ID: "P1", Seat: 0, Hand: []Card{
		{Suit: Hearts, Rank: Ace}, // Trump
		{Suit: Clubs, Rank: Two},  // Non-trump
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
	t.Parallel()
	g := New("test-force")
	g.Players[1] = &Player{ID: "P2", Seat: 1, Hand: []Card{
		{Suit: None, Rank: Joker},
		{Suit: Hearts, Rank: Two},
	}}
	g.Status = PhasePlaying
	g.CurrentTurn = 1
	g.Tricks = []Trick{{}, {
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

func TestFirstTrickMightyFollowSuitRestriction(t *testing.T) {
	t.Parallel()
	g := New("test-first-trick-mighty")
	g.Status = PhasePlaying
	g.Trump = Hearts // Mighty is Ace of Spades
	g.CurrentTurn = 1
	g.Players[0] = &Player{ID: "P1", Seat: 0}
	g.Players[1] = &Player{ID: "P2", Seat: 1}
	g.Tricks = []Trick{{
		Cards: []PlayedCard{
			{PlayerID: "P1", Seat: 0, Card: Card{Suit: Hearts, Rank: King}},
		},
		LeadSuit: Hearts,
	}}

	// Has lead suit in hand, so off-suit Mighty is not allowed on trick 1.
	g.Players[1].Hand = []Card{
		{Suit: Spades, Rank: Ace}, // Mighty
		{Suit: Hearts, Rank: Two}, // Can follow suit
	}

	err := g.ValidateMove("P2", MovePlayCard, PlayCardMove{Card: Card{Suit: Spades, Rank: Ace}})
	if err == nil {
		t.Fatalf("expected first-trick mighty rejection when player can follow suit")
	}

	// No lead suit available, so Mighty is rejected on the first trick.
	g.Players[1].Hand = []Card{
		{Suit: Spades, Rank: Ace}, // Mighty
		{Suit: Clubs, Rank: Two},
	}

	err = g.ValidateMove("P2", MovePlayCard, PlayCardMove{Card: Card{Suit: Spades, Rank: Ace}})
	if err == nil {
		t.Fatalf("expected mighty to be rejected on first trick when lead is a different suit: %v", err)
	}
}

func TestScoring(t *testing.T) {
	t.Parallel()
	g := New("test-scoring")
	g.Declarer = 0
	// Friend is seat 1: place the called card in seat 1's hand so friendSeat()
	// resolves to 1 (scoring no longer reads PartnerSeat).
	g.PartnerCard = &Card{Suit: Hearts, Rank: King}
	g.Players[1] = &Player{ID: "p1", Seat: 1, Hand: []Card{{Suit: Hearts, Rank: King}}}
	g.Contract = &Bid{Points: 7, Suit: Spades, IsNoTrump: false}

	// Simulate 8 tricks won by team (Declarer + Partner)
	for range 8 {
		g.Tricks = append(g.Tricks, Trick{Winner: 0})
	}
	// 2 tricks won by opponents
	for range 2 {
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
	for range 10 {
		g.Tricks = append(g.Tricks, Trick{Winner: 0})
	}
	// 10-bid, 10 tricks, NT, NoFriend -> (10*10)*2*2*2 = 800
	score, _ = g.CalculateFinalScore()
	if score != 800 {
		t.Errorf("Expected ultimate score 800, got %v", score)
	}
}

func TestValidateBid_NoTrumpAndSuitValidation(t *testing.T) {
	t.Parallel()
	g := New("test-bid-validation")
	g.Status = PhaseBidding
	g.CurrentTurn = 0
	g.Players[0] = &Player{ID: "P1", Seat: 0}

	if err := g.ValidateMove("P1", MoveBid, Bid{Points: 7, Suit: Suit("invalid")}); err == nil {
		t.Fatalf("expected invalid suit bid to be rejected")
	}

	if err := g.ValidateMove("P1", MoveBid, Bid{Points: 7, Suit: Spades, IsNoTrump: true}); err == nil {
		t.Fatalf("expected no-trump bid with non-none suit to be rejected")
	}

	g.CurrentBid = &Bid{Points: 8, Suit: None, IsNoTrump: true}
	if err := g.ValidateMove("P1", MoveBid, Bid{Points: 8, Suit: None, IsNoTrump: true}); err == nil {
		t.Fatalf("expected equal no-trump bid to be rejected")
	}
}

func TestValidateBid_StrictlyIncreasing(t *testing.T) {
	t.Parallel()
	g := New("test-strict-bid")
	g.Status = PhaseBidding
	g.CurrentTurn = 0
	g.Players[0] = &Player{ID: "P1", Seat: 0}

	// Given a current bid of 7 Clubs
	g.CurrentBid = &Bid{Points: 7, Suit: Clubs, IsNoTrump: false}

	// A bid of 7 Spades (higher suit rank) should be rejected
	if err := g.ValidateMove("P1", MoveBid, Bid{Points: 7, Suit: Spades, IsNoTrump: false}); err == nil {
		t.Fatalf("expected 7 Spades over 7 Clubs to be rejected (points must be strictly higher)")
	}

	// A bid of 7 No-Trump should be rejected
	if err := g.ValidateMove("P1", MoveBid, Bid{Points: 7, Suit: None, IsNoTrump: true}); err == nil {
		t.Fatalf("expected 7 NT over 7 Clubs to be rejected (points must be strictly higher)")
	}

	// A bid of 8 Clubs should be accepted
	if err := g.ValidateMove("P1", MoveBid, Bid{Points: 8, Suit: Clubs, IsNoTrump: false}); err != nil {
		t.Fatalf("expected 8 Clubs over 7 Clubs to be accepted, got: %v", err)
	}
}

func TestApplyMove_MaxBidAutoResolves(t *testing.T) {
	t.Parallel()
	g := New("test-max-bid")
	for i := range 5 {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i, Name: string(rune('A' + i))}
	}
	g.Start() // deals cards and sets PhaseBidding

	playerID := g.Players[g.CurrentTurn].ID
	err := g.ApplyMove(playerID, MoveBid, Bid{Points: 10, Suit: Spades, IsNoTrump: false})
	if err != nil {
		t.Fatalf("failed to apply 10 point bid: %v", err)
	}

	if g.Status != PhaseExchanging {
		t.Fatalf("expected phase to immediately become PhaseExchanging, got %s", g.Status)
	}
	if g.Declarer != g.GetPlayer(playerID).Seat {
		t.Fatalf("expected declarer to be set correctly")
	}
	if g.Contract == nil || g.Contract.Points != 10 {
		t.Fatalf("expected contract to be finalized")
	}
	if len(g.Kitty) != 0 {
		t.Fatalf("expected kitty to be emptied into declarer's hand")
	}
}

func TestApplyMove_SkipPassedBidders(t *testing.T) {
	g := New("test-game")
	// Add 5 players
	for i := range 5 {
		p := &Player{ID: fmt.Sprintf("player%d", i+1), Seat: i, Name: fmt.Sprintf("P%d", i+1)}
		g.Players[i] = p
	}
	g.Start()

	// Player 1 bids
	bid1 := Bid{Suit: Clubs, Points: 3}
	err := g.ApplyMove("player1", MoveBid, bid1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Player 2 passes
	err = g.ApplyMove("player2", MovePass, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	// Player 3 passes
	err = g.ApplyMove("player3", MovePass, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Player 4 bids
	bid2 := Bid{Suit: Diamonds, Points: 4}
	err = g.ApplyMove("player4", MoveBid, bid2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After player 4 bids, it should be player 5's turn
	if g.CurrentTurn != 4 {
		t.Errorf("Expected current turn 4 (player 5), got %d", g.CurrentTurn)
	}

	// Player 5 passes
	err = g.ApplyMove("player5", MovePass, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Now it should be player 1's turn
	if g.CurrentTurn != 0 {
		t.Errorf("Expected current turn 0 (player 1), got %d", g.CurrentTurn)
	}

	// Player 1 passes, it should skip player 2 and 3 and be player 4's turn
	err = g.ApplyMove("player1", MovePass, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if g.CurrentTurn != 3 {
		t.Errorf("Expected current turn 3 (player 4) after skipping, got %d", g.CurrentTurn)
	}
}

func TestApplyMove_AllFivePassRedeal(t *testing.T) {
	t.Parallel()
	g := New("test-redeal")
	for i := range 5 {
		p := &Player{ID: fmt.Sprintf("player%d", i+1), Seat: i, Name: fmt.Sprintf("P%d", i+1)}
		g.Players[i] = p
	}
	g.Start()

	initialVersion := g.Version

	// All 5 players pass sequentially
	for i := range 5 {
		playerID := fmt.Sprintf("player%d", i+1)
		err := g.ApplyMove(playerID, MovePass, nil)
		if err != nil {
			t.Fatalf("unexpected error on pass %d: %v", i+1, err)
		}
	}

	// Should have redealt: meaning Phase is Bidding again, kitty is back, etc.
	if g.Status != PhaseBidding {
		t.Errorf("Expected PhaseBidding after 5 passes, got %s", g.Status)
	}
	if g.Version <= initialVersion {
		t.Errorf("Expected version to increase")
	}
	if len(g.Kitty) != 3 {
		t.Errorf("Expected kitty to be recreated with 3 cards, got %d", len(g.Kitty))
	}
	if len(g.PassedPlayers) != 0 {
		t.Errorf("Expected passed players to be reset")
	}
}

func TestApplyMove_FourPassThenBid(t *testing.T) {
	g := New("test-game-four-pass-then-bid")
	for i := range 5 {
		p := &Player{ID: fmt.Sprintf("player%d", i+1), Seat: i, Name: fmt.Sprintf("P%d", i+1)}
		g.Players[i] = p
	}
	g.Start()

	// Players 1, 2, 3, 4 pass
	for i := range 4 {
		playerID := fmt.Sprintf("player%d", i+1)
		err := g.ApplyMove(playerID, MovePass, nil)
		if err != nil {
			t.Fatalf("unexpected error on pass %d: %v", i+1, err)
		}
	}

	// Player 5 makes a bid under 10 (e.g. 5)
	err := g.ApplyMove("player5", MoveBid, Bid{Suit: Spades, Points: 5})
	if err != nil {
		t.Fatalf("unexpected error on bid: %v", err)
	}

	// The game should enter PhaseExchanging immediately
	if g.Status != PhaseExchanging {
		t.Errorf("Expected Exchanging phase, got %s", g.Status)
	}
	if g.Declarer != 4 {
		t.Errorf("Expected Declarer to be 4 (Player 5), got %d", g.Declarer)
	}
	if g.CurrentBid.Points != 5 {
		t.Errorf("Expected bid to be 5, got %d", g.CurrentBid.Points)
	}
}
