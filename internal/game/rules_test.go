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

// scoringGame builds a finished-hand game where the caller team has captured
// `teamPoints` scoring cards. Seats 0-4 are all filled. Seat 0 is the declarer.
// With a friend, seat 1 holds the called card; secretSolo puts it in seat 0's
// own hand so friendSeat() resolves to the declarer.
func scoringGame(bid int, noTrump, noFriend, secretSolo bool, teamPoints int) *Game {
	g := New("score")
	g.Declarer = 0
	g.Contract = &Bid{Points: bid, Suit: Spades, IsNoTrump: noTrump}
	for i := 0; i < 5; i++ {
		g.Players[i] = &Player{ID: fmt.Sprintf("p%d", i), Seat: i}
	}
	switch {
	case noFriend:
		g.IsNoFriend = true
	case secretSolo:
		g.PartnerCard = &Card{Suit: Hearts, Rank: King}
		g.Players[0].Hand = []Card{{Suit: Hearts, Rank: King}}
	default:
		g.PartnerCard = &Card{Suit: Hearts, Rank: King}
		g.Players[1].Hand = []Card{{Suit: Hearts, Rank: King}}
	}
	// All the team's captured scoring cards sit on the declarer's pile; only
	// the length is read, so zero-value cards are fine.
	g.Players[0].Points = make([]Card, teamPoints)
	return g
}

func TestCalculateFinalScore(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                    string
		bid                     int
		noTrump, noFriend, solo bool
		teamPoints              int
		wantDeclarer            int
		wantPartner             int // meaningful only when a partner exists
		wantOpp                 int
	}{
		{"success 15d 16pts", 5, false, false, false, 16, 10, 5, -5},
		{"fail 15d 13pts", 5, false, false, false, 13, -4, -2, 2},
		{"success 16nt 18pts", 6, true, false, false, 18, 32, 16, -16},
		{"fail 16nt 13pts", 6, true, false, false, 13, -12, -6, 6},
		{"run 17h 20pts", 7, false, false, false, 20, 44, 22, -22},
		{"alone 16nt 17pts", 6, true, true, false, 17, 112, 0, -28},
		{"alone fail 16nt 15pts", 6, true, true, false, 15, -16, 0, 4},
		{"zero payment bid3 13pts", 3, false, false, false, 13, 0, 0, 0},
		{"back run bid5 9pts", 5, false, false, false, 9, -24, -12, 12},
		{"secret solo 15s 16pts", 5, false, false, true, 16, 20, 0, -5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := scoringGame(tc.bid, tc.noTrump, tc.noFriend, tc.solo, tc.teamPoints)
			got := g.CalculateFinalScore()

			if got[0] != tc.wantDeclarer {
				t.Errorf("declarer: got %d, want %d", got[0], tc.wantDeclarer)
			}
			if !tc.noFriend && !tc.solo && got[1] != tc.wantPartner {
				t.Errorf("partner: got %d, want %d", got[1], tc.wantPartner)
			}
			// Every opponent seat pays/collects the same amount.
			for _, seat := range oppSeats(g) {
				if got[seat] != tc.wantOpp {
					t.Errorf("opp seat %d: got %d, want %d", seat, got[seat], tc.wantOpp)
				}
			}
			// Zero-sum invariant.
			sum := 0
			for _, v := range got {
				sum += v
			}
			if sum != 0 {
				t.Errorf("scores must sum to zero, got %d (%v)", sum, got)
			}
		})
	}
}

// oppSeats returns the seats that are neither the declarer nor a revealed partner.
func oppSeats(g *Game) []int {
	declarer := g.Declarer
	fs := g.friendSeat()
	partner := fs >= 0 && fs != declarer
	var seats []int
	for seat, p := range g.Players {
		if p == nil || seat == declarer || (partner && seat == fs) {
			continue
		}
		seats = append(seats, seat)
	}
	return seats
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
