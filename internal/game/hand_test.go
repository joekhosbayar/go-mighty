package game

import (
	"testing"
)

// TestNewHand tests hand creation
// Rule: New hand should initialize with correct dealer and phase
func TestNewHand(t *testing.T) {
	hand := NewHand(1, 2, 5)

	if hand.HandNo != 1 {
		t.Errorf("HandNo = %d, want 1", hand.HandNo)
	}
	if hand.DealerSeat != 2 {
		t.Errorf("DealerSeat = %d, want 2", hand.DealerSeat)
	}
	if hand.Phase != PhaseBidding {
		t.Errorf("Phase = %v, want PhaseBidding", hand.Phase)
	}
	if hand.CurrentBidder != 2 {
		t.Errorf("CurrentBidder = %d, want 2 (dealer)", hand.CurrentBidder)
	}
	if hand.DeclarerSeat != -1 {
		t.Errorf("DeclarerSeat = %d, want -1 (unset)", hand.DeclarerSeat)
	}
	if hand.PartnerSeat != -1 {
		t.Errorf("PartnerSeat = %d, want -1 (unset)", hand.PartnerSeat)
	}
	if len(hand.PlayerHands) != 5 {
		t.Errorf("len(PlayerHands) = %d, want 5", len(hand.PlayerHands))
	}
}

// TestHand_AddBid_Success tests successful bid addition
// Rule: Valid bids should be added and tracked
func TestHand_AddBid_Success(t *testing.T) {
	hand := NewHand(1, 0, 5)

	bid := NewBid(0, 13, Trump{Suit: Hearts, NoTrump: false})
	err := hand.AddBid(bid)

	if err != nil {
		t.Errorf("AddBid returned error: %v", err)
	}
	if len(hand.Bids) != 1 {
		t.Errorf("len(Bids) = %d, want 1", len(hand.Bids))
	}
	if hand.HighestBid == nil {
		t.Fatal("HighestBid should not be nil")
	}
	if hand.HighestBid.Points != 13 {
		t.Errorf("HighestBid.Points = %d, want 13", hand.HighestBid.Points)
	}
}

// TestHand_AddBid_WrongPhase tests bid rejection in wrong phase
// Rule: Can only bid during bidding phase
func TestHand_AddBid_WrongPhase(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhasePlaying // Wrong phase

	bid := NewBid(0, 13, Trump{Suit: Hearts, NoTrump: false})
	err := hand.AddBid(bid)

	if err != ErrInvalidPhase {
		t.Errorf("AddBid error = %v, want ErrInvalidPhase", err)
	}
}

// TestHand_AddBid_NotPlayerTurn tests bid rejection when not player's turn
// Rule: Only current bidder can bid
func TestHand_AddBid_NotPlayerTurn(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.CurrentBidder = 1

	bid := NewBid(0, 13, Trump{Suit: Hearts, NoTrump: false}) // Player 0 bidding when it's player 1's turn
	err := hand.AddBid(bid)

	if err != ErrNotPlayerTurn {
		t.Errorf("AddBid error = %v, want ErrNotPlayerTurn", err)
	}
}

// TestHand_AddBid_AlreadyPassed tests bid rejection after passing
// Rule: Once a player passes, they cannot bid again
func TestHand_AddBid_AlreadyPassed(t *testing.T) {
	hand := NewHand(1, 0, 5)

	// Player passes
	pass := NewPass(0)
	hand.AddBid(pass)
	hand.NextBidder(5)

	// Try to bid after passing
	hand.CurrentBidder = 0
	bid := NewBid(0, 15, Trump{Suit: Hearts, NoTrump: false})
	err := hand.AddBid(bid)

	if err != ErrPlayerAlreadyPassed {
		t.Errorf("AddBid error = %v, want ErrPlayerAlreadyPassed", err)
	}
}

// TestHand_AddBid_BidTooLow tests rejection of bids that aren't higher
// Rule: Each bid must be higher than the current highest bid
func TestHand_AddBid_BidTooLow(t *testing.T) {
	hand := NewHand(1, 0, 5)

	// First bid: 15 hearts
	bid1 := NewBid(0, 15, Trump{Suit: Hearts, NoTrump: false})
	hand.AddBid(bid1)
	hand.NextBidder(5)

	// Try to bid 14 (lower)
	bid2 := NewBid(1, 14, Trump{Suit: Spades, NoTrump: false})
	err := hand.AddBid(bid2)

	if err != ErrBidTooLow {
		t.Errorf("AddBid error = %v, want ErrBidTooLow", err)
	}
}

// TestHand_AddBid_InvalidPoints tests rejection of out-of-range bids
// Rule: Bid points must be between 13 and 20
func TestHand_AddBid_InvalidPoints(t *testing.T) {
	hand := NewHand(1, 0, 5)

	tests := []struct {
		name   string
		points int
	}{
		{"Bid too low (12)", 12},
		{"Bid too high (21)", 21},
		{"Bid way too low (5)", 5},
		{"Bid way too high (100)", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bid := NewBid(0, tt.points, Trump{Suit: Hearts, NoTrump: false})
			err := hand.AddBid(bid)

			if err != ErrInvalidBid {
				t.Errorf("AddBid(%d points) error = %v, want ErrInvalidBid", tt.points, err)
			}
		})
	}
}

// TestHand_AddBid_NoTrumpBeatssuit tests no-trump bid superiority
// Rule: No-trump beats same points with a suit
func TestHand_AddBid_NoTrumpBeatsSuit(t *testing.T) {
	hand := NewHand(1, 0, 5)

	// First bid: 15 hearts
	bid1 := NewBid(0, 15, Trump{Suit: Hearts, NoTrump: false})
	hand.AddBid(bid1)
	hand.NextBidder(5)

	// Second bid: 15 no-trump (should be valid)
	bid2 := NewBid(1, 15, Trump{Suit: "", NoTrump: true})
	err := hand.AddBid(bid2)

	if err != nil {
		t.Errorf("AddBid(15 no-trump) error = %v, want nil", err)
	}
	if hand.HighestBid.Points != 15 || !hand.HighestBid.Trump.NoTrump {
		t.Error("15 no-trump should be the highest bid")
	}
}

// TestHand_AddBid_Pass tests passing functionality
// Rule: Players can pass, and passed players are tracked
func TestHand_AddBid_Pass(t *testing.T) {
	hand := NewHand(1, 0, 5)

	pass := NewPass(0)
	err := hand.AddBid(pass)

	if err != nil {
		t.Errorf("AddBid(pass) error = %v", err)
	}
	if !hand.PassedSeats[0] {
		t.Error("Player 0 should be marked as passed")
	}
	if hand.HighestBid != nil {
		t.Error("HighestBid should remain nil after pass with no previous bids")
	}
}

// TestHand_NextBidder tests bidder rotation
// Rule: Bidding proceeds clockwise
func TestHand_NextBidder(t *testing.T) {
	hand := NewHand(1, 0, 5)

	// Start at seat 0
	if hand.CurrentBidder != 0 {
		t.Errorf("Initial CurrentBidder = %d, want 0", hand.CurrentBidder)
	}

	// Advance to seat 1
	next := hand.NextBidder(5)
	if next != 1 {
		t.Errorf("NextBidder() = %d, want 1", next)
	}
	if hand.CurrentBidder != 1 {
		t.Errorf("CurrentBidder = %d, want 1", hand.CurrentBidder)
	}

	// Advance through all seats and wrap around
	for i := 2; i < 5; i++ {
		hand.NextBidder(5)
	}
	next = hand.NextBidder(5)
	if next != 0 {
		t.Errorf("NextBidder() after wrap = %d, want 0", next)
	}
}

// TestHand_IsBiddingComplete_AllPassed tests bidding end with all passes
// Rule: If all players pass, bidding is complete (redeal)
func TestHand_IsBiddingComplete_AllPassed(t *testing.T) {
	hand := NewHand(1, 0, 5)

	// All 5 players pass
	for i := 0; i < 5; i++ {
		hand.CurrentBidder = i
		hand.AddBid(NewPass(i))
	}

	if !hand.IsBiddingComplete(5) {
		t.Error("Bidding should be complete when all players pass")
	}
}

// TestHand_IsBiddingComplete_OnlyOneBidder tests bidding end with one active bidder
// Rule: When only one player hasn't passed, bidding is complete
func TestHand_IsBiddingComplete_OnlyOneBidder(t *testing.T) {
	hand := NewHand(1, 0, 5)

	// Player 0 bids
	hand.CurrentBidder = 0
	hand.AddBid(NewBid(0, 13, Trump{Suit: Hearts, NoTrump: false}))

	// Players 1, 2, 3, 4 pass
	for i := 1; i < 5; i++ {
		hand.CurrentBidder = i
		hand.AddBid(NewPass(i))
	}

	if !hand.IsBiddingComplete(5) {
		t.Error("Bidding should be complete when only one player remains")
	}
}

// TestHand_FinalizeBidding tests contract finalization
// Rule: Bidding winner becomes declarer and contract is set
func TestHand_FinalizeBidding(t *testing.T) {
	hand := NewHand(1, 0, 5)

	// Player 2 makes highest bid
	hand.CurrentBidder = 2
	hand.AddBid(NewBid(2, 16, Trump{Suit: Spades, NoTrump: false}))

	// Others pass
	hand.PassedSeats[0] = true
	hand.PassedSeats[1] = true
	hand.PassedSeats[3] = true
	hand.PassedSeats[4] = true

	err := hand.FinalizeBidding()

	if err != nil {
		t.Errorf("FinalizeBidding error = %v", err)
	}
	if hand.DeclarerSeat != 2 {
		t.Errorf("DeclarerSeat = %d, want 2", hand.DeclarerSeat)
	}
	if hand.Contract == nil {
		t.Fatal("Contract should not be nil")
	}
	if hand.Contract.Points != 16 {
		t.Errorf("Contract.Points = %d, want 16", hand.Contract.Points)
	}
	if hand.Phase != PhaseKitty {
		t.Errorf("Phase = %v, want PhaseKitty", hand.Phase)
	}
}

// TestHand_FinalizeBidding_NoBids tests error when no bids made
// Rule: Cannot finalize if no one bid
func TestHand_FinalizeBidding_NoBids(t *testing.T) {
	hand := NewHand(1, 0, 5)

	err := hand.FinalizeBidding()

	if err != ErrInvalidBid {
		t.Errorf("FinalizeBidding error = %v, want ErrInvalidBid", err)
	}
}

// TestHand_PickupKitty tests declarer picking up kitty
// Rule: Declarer adds 3 kitty cards to their hand
func TestHand_PickupKitty(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhaseKitty
	hand.DeclarerSeat = 2

	// Setup hands
	hand.PlayerHands = make([][]Card, 5)
	for i := 0; i < 5; i++ {
		hand.PlayerHands[i] = make([]Card, 10)
	}

	// Setup kitty
	hand.Kitty = []Card{
		{Suit: Spades, Rank: Ace},
		{Suit: Hearts, Rank: King},
		{Suit: Clubs, Rank: Queen},
	}

	err := hand.PickupKitty()

	if err != nil {
		t.Errorf("PickupKitty error = %v", err)
	}
	if len(hand.PlayerHands[2]) != 13 {
		t.Errorf("Declarer has %d cards, want 13", len(hand.PlayerHands[2]))
	}
	if hand.Phase != PhaseDiscard {
		t.Errorf("Phase = %v, want PhaseDiscard", hand.Phase)
	}
}

// TestHand_PickupKitty_WrongPhase tests kitty pickup in wrong phase
// Rule: Can only pickup kitty in PhaseKitty
func TestHand_PickupKitty_WrongPhase(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhaseBidding // Wrong phase

	err := hand.PickupKitty()

	if err != ErrInvalidPhase {
		t.Errorf("PickupKitty error = %v, want ErrInvalidPhase", err)
	}
}

// TestHand_Discard tests declarer discarding cards
// Rule: Declarer must discard exactly 3 cards from their hand
func TestHand_Discard(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhaseDiscard
	hand.DeclarerSeat = 1

	// Setup declarer's hand with 13 cards
	hand.PlayerHands = make([][]Card, 5)
	hand.PlayerHands[1] = []Card{
		{Suit: Spades, Rank: Two},
		{Suit: Spades, Rank: Three},
		{Suit: Spades, Rank: Four},
		{Suit: Hearts, Rank: Five},
		{Suit: Hearts, Rank: Six},
		{Suit: Clubs, Rank: Seven},
		{Suit: Clubs, Rank: Eight},
		{Suit: Diamonds, Rank: Nine},
		{Suit: Diamonds, Rank: Ten},
		{Suit: Spades, Rank: Jack},
		{Suit: Hearts, Rank: Queen},
		{Suit: Clubs, Rank: King},
		{Suit: Diamonds, Rank: Ace},
	}

	// Discard 3 cards
	discardCards := []Card{
		{Suit: Spades, Rank: Two},
		{Suit: Hearts, Rank: Five},
		{Suit: Clubs, Rank: Seven},
	}

	err := hand.Discard(discardCards)

	if err != nil {
		t.Errorf("Discard error = %v", err)
	}
	if len(hand.PlayerHands[1]) != 10 {
		t.Errorf("After discard, declarer has %d cards, want 10", len(hand.PlayerHands[1]))
	}
	if len(hand.Discarded) != 3 {
		t.Errorf("len(Discarded) = %d, want 3", len(hand.Discarded))
	}
	if hand.Phase != PhaseCallingPartner {
		t.Errorf("Phase = %v, want PhaseCallingPartner", hand.Phase)
	}
}

// TestHand_Discard_WrongCount tests discard with wrong number of cards
// Rule: Must discard exactly 3 cards
func TestHand_Discard_WrongCount(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhaseDiscard
	hand.DeclarerSeat = 1
	hand.PlayerHands = make([][]Card, 5)
	hand.PlayerHands[1] = make([]Card, 13)

	tests := []struct {
		name  string
		count int
	}{
		{"Discard 2 cards", 2},
		{"Discard 4 cards", 4},
		{"Discard 0 cards", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discardCards := make([]Card, tt.count)
			err := hand.Discard(discardCards)

			if err != ErrInvalidMove {
				t.Errorf("Discard(%d cards) error = %v, want ErrInvalidMove", tt.count, err)
			}
		})
	}
}

// TestHand_Discard_CardNotInHand tests discard of cards not in hand
// Rule: Can only discard cards actually in declarer's hand
func TestHand_Discard_CardNotInHand(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhaseDiscard
	hand.DeclarerSeat = 1

	hand.PlayerHands = make([][]Card, 5)
	hand.PlayerHands[1] = []Card{
		{Suit: Spades, Rank: Two},
		{Suit: Hearts, Rank: Three},
	}

	// Try to discard a card not in hand
	discardCards := []Card{
		{Suit: Spades, Rank: Two},   // In hand
		{Suit: Hearts, Rank: Three}, // In hand
		{Suit: Clubs, Rank: Ace},    // NOT in hand
	}

	err := hand.Discard(discardCards)

	if err != ErrCardNotInHand {
		t.Errorf("Discard error = %v, want ErrCardNotInHand", err)
	}
}

// TestHand_CallPartner tests partner calling
// Rule: Declarer can call partner by card, first trick, or no friend
func TestHand_CallPartner(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhaseCallingPartner
	hand.Contract = &Contract{
		DeclarerSeat: 0,
		Points:       15,
		Trump:        Trump{Suit: Hearts, NoTrump: false},
	}

	callCard := Card{Suit: Spades, Rank: Ace}
	call := PartnerCall{
		Type: PartnerCallCard,
		Card: &callCard,
	}

	err := hand.CallPartner(call)

	if err != nil {
		t.Errorf("CallPartner error = %v", err)
	}
	if hand.Contract.PartnerCall == nil {
		t.Fatal("PartnerCall should not be nil")
	}
	if hand.Phase != PhasePlaying {
		t.Errorf("Phase = %v, want PhasePlaying", hand.Phase)
	}
}

// TestHand_CallPartner_NoFriend tests no-friend partner call
// Rule: No-friend sets contract flag and partner seat to -1
func TestHand_CallPartner_NoFriend(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhaseCallingPartner
	hand.Contract = &Contract{
		DeclarerSeat: 0,
		Points:       15,
		Trump:        Trump{Suit: Hearts, NoTrump: false},
	}

	call := PartnerCall{
		Type: PartnerCallNoFriend,
	}

	err := hand.CallPartner(call)

	if err != nil {
		t.Errorf("CallPartner(no friend) error = %v", err)
	}
	if !hand.Contract.NoFriend {
		t.Error("Contract.NoFriend should be true")
	}
	if hand.PartnerSeat != -1 {
		t.Errorf("PartnerSeat = %d, want -1 for no friend", hand.PartnerSeat)
	}
}

// TestHand_StartTrick tests starting a new trick
// Rule: New trick should be initialized with leader
func TestHand_StartTrick(t *testing.T) {
	hand := NewHand(1, 0, 5)

	hand.StartTrick(2)

	if hand.CurrentTrick == nil {
		t.Fatal("CurrentTrick should not be nil")
	}
	if hand.CurrentTrick.LeaderSeat != 2 {
		t.Errorf("CurrentTrick.LeaderSeat = %d, want 2", hand.CurrentTrick.LeaderSeat)
	}
	if hand.CurrentTrick.TrickNo != 1 {
		t.Errorf("CurrentTrick.TrickNo = %d, want 1", hand.CurrentTrick.TrickNo)
	}
}

// TestHand_PlayCard tests playing a card
// Rule: Players play cards in turn order
func TestHand_PlayCard(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhasePlaying

	// Setup hands
	hand.PlayerHands = make([][]Card, 5)
	hand.PlayerHands[0] = []Card{
		{Suit: Spades, Rank: Ace},
		{Suit: Hearts, Rank: King},
	}

	// Start trick with player 0 as leader
	hand.StartTrick(0)

	// Player 0 plays
	err := hand.PlayCard(0, Card{Suit: Spades, Rank: Ace}, 5)

	if err != nil {
		t.Errorf("PlayCard error = %v", err)
	}
	if len(hand.CurrentTrick.Cards) != 1 {
		t.Errorf("CurrentTrick has %d cards, want 1", len(hand.CurrentTrick.Cards))
	}
	if len(hand.PlayerHands[0]) != 1 {
		t.Errorf("Player 0 has %d cards left, want 1", len(hand.PlayerHands[0]))
	}
}

// TestHand_PlayCard_WrongTurn tests playing out of turn
// Rule: Only the expected player can play
func TestHand_PlayCard_WrongTurn(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhasePlaying

	hand.PlayerHands = make([][]Card, 5)
	hand.PlayerHands[1] = []Card{{Suit: Spades, Rank: Ace}}

	// Start trick with player 0 as leader
	hand.StartTrick(0)

	// Player 1 tries to play when it's player 0's turn
	err := hand.PlayCard(1, Card{Suit: Spades, Rank: Ace}, 5)

	if err != ErrNotPlayerTurn {
		t.Errorf("PlayCard error = %v, want ErrNotPlayerTurn", err)
	}
}

// TestHand_PlayCard_CardNotInHand tests playing a card not in hand
// Rule: Can only play cards actually in your hand
func TestHand_PlayCard_CardNotInHand(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhasePlaying

	hand.PlayerHands = make([][]Card, 5)
	hand.PlayerHands[0] = []Card{
		{Suit: Hearts, Rank: King},
	}

	hand.StartTrick(0)

	// Try to play a card not in hand
	err := hand.PlayCard(0, Card{Suit: Spades, Rank: Ace}, 5)

	if err != ErrCardNotInHand {
		t.Errorf("PlayCard error = %v, want ErrCardNotInHand", err)
	}
}

// TestHand_CompleteTrick tests trick completion and winner determination
// Rule: After all players play, trick winner is determined
func TestHand_CompleteTrick(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Phase = PhasePlaying
	hand.Contract = &Contract{
		Trump: Trump{Suit: Hearts, NoTrump: false},
	}

	// Setup a complete trick
	hand.StartTrick(0)
	hand.CurrentTrick.AddCard(0, Card{Suit: Spades, Rank: Two})
	hand.CurrentTrick.AddCard(1, Card{Suit: Spades, Rank: Ace})
	hand.CurrentTrick.AddCard(2, Card{Suit: Spades, Rank: King})
	hand.CurrentTrick.AddCard(3, Card{Suit: Spades, Rank: Queen})
	hand.CurrentTrick.AddCard(4, Card{Suit: Spades, Rank: Jack})

	winnerSeat, err := hand.CompleteTrick(5)

	if err != nil {
		t.Errorf("CompleteTrick error = %v", err)
	}
	// Ace should win
	if winnerSeat != 1 {
		t.Errorf("Winner = seat %d, want seat 1 (Ace)", winnerSeat)
	}
	if len(hand.Tricks) != 1 {
		t.Errorf("len(Tricks) = %d, want 1", len(hand.Tricks))
	}
	if hand.CurrentTrick != nil {
		t.Error("CurrentTrick should be nil after completion")
	}
}

// TestHand_IsComplete tests hand completion detection
// Rule: Hand is complete after 10 tricks
func TestHand_IsComplete(t *testing.T) {
	hand := NewHand(1, 0, 5)

	// Add 9 tricks
	for i := 0; i < 9; i++ {
		hand.Tricks = append(hand.Tricks, NewTrick(i+1, 0))
	}

	if hand.IsComplete() {
		t.Error("Hand should not be complete after 9 tricks")
	}

	// Add 10th trick
	hand.Tricks = append(hand.Tricks, NewTrick(10, 0))

	if !hand.IsComplete() {
		t.Error("Hand should be complete after 10 tricks")
	}
}

// TestHand_CalculateHandPoints tests point calculation
// Rule: Declarer team's points include declarer + partner + discarded cards
func TestHand_CalculateHandPoints(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.DeclarerSeat = 1
	hand.PartnerSeat = 3
	hand.Contract = &Contract{NoFriend: false}

	// Set points by seat
	hand.PointsBySeat = map[int]int{
		0: 2,
		1: 5, // Declarer
		2: 1,
		3: 4, // Partner
		4: 3,
	}

	// Add discarded card points
	hand.Discarded = []Card{
		{Suit: Spades, Rank: Ace},  // 1 point
		{Suit: Hearts, Rank: King}, // 1 point
		{Suit: Clubs, Rank: Two},   // 0 points
	}

	totalPoints := hand.CalculateHandPoints()

	// Should be 5 (declarer) + 4 (partner) + 2 (discarded) = 11
	if totalPoints != 11 {
		t.Errorf("CalculateHandPoints() = %d, want 11", totalPoints)
	}
}

// TestHand_CalculateHandPoints_NoFriend tests points with no friend
// Rule: With no friend, only declarer's points count (plus discarded)
func TestHand_CalculateHandPoints_NoFriend(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.DeclarerSeat = 1
	hand.PartnerSeat = -1
	hand.Contract = &Contract{NoFriend: true}

	hand.PointsBySeat = map[int]int{
		0: 2,
		1: 8, // Declarer
		2: 3,
		3: 4,
		4: 3,
	}

	hand.Discarded = []Card{
		{Suit: Spades, Rank: Ace}, // 1 point
	}

	totalPoints := hand.CalculateHandPoints()

	// Should be 8 (declarer) + 1 (discarded) = 9 (partner not counted)
	if totalPoints != 9 {
		t.Errorf("CalculateHandPoints() with no friend = %d, want 9", totalPoints)
	}
}

// TestCalculateWeakHandValue tests weak hand calculation for redeal
// Rule: Mighty=0, Joker=-1, A/K/Q/J=+1, 10=+0.5, others=0
func TestCalculateWeakHandValue(t *testing.T) {
	tests := []struct {
		name     string
		hand     []Card
		expected float64
	}{
		{
			name: "Strong hand should have high value",
			hand: []Card{
				{Suit: Spades, Rank: Ace},    // SA (Mighty) = 0
				{Suit: Hearts, Rank: Ace},    // +1
				{Suit: Diamonds, Rank: King}, // +1
				{Suit: Clubs, Rank: Queen},   // +1
			},
			expected: 3.0,
		},
		{
			name: "Weak hand with Joker should have negative value",
			hand: []Card{
				{Suit: NoSuit, Rank: Joker}, // -1
				{Suit: Hearts, Rank: Two},   // 0
				{Suit: Clubs, Rank: Three},  // 0
			},
			expected: -1.0,
		},
		{
			name: "Hand with Ten should add 0.5",
			hand: []Card{
				{Suit: Hearts, Rank: Ten},    // +0.5
				{Suit: Diamonds, Rank: Jack}, // +1
			},
			expected: 1.5,
		},
		{
			name: "Very weak hand qualifies for redeal",
			hand: []Card{
				{Suit: NoSuit, Rank: Joker},   // -1
				{Suit: Hearts, Rank: Ten},     // +0.5
				{Suit: Clubs, Rank: Two},      // 0
				{Suit: Diamonds, Rank: Three}, // 0
			},
			expected: -0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateWeakHandValue(tt.hand, Spades)
			if result != tt.expected {
				t.Errorf("CalculateWeakHandValue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestHand_CanRedealForWeakHand tests redeal eligibility
// Rule: Can redeal if hand value ≤ 0.5 points
func TestHand_CanRedealForWeakHand(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.PlayerHands = make([][]Card, 5)

	// Weak hand (≤ 0.5)
	hand.PlayerHands[0] = []Card{
		{Suit: NoSuit, Rank: Joker}, // -1
		{Suit: Hearts, Rank: Ten},   // +0.5
		{Suit: Clubs, Rank: Two},    // 0
	}

	// Strong hand
	hand.PlayerHands[1] = []Card{
		{Suit: Hearts, Rank: Ace},    // +1
		{Suit: Diamonds, Rank: King}, // +1
		{Suit: Clubs, Rank: Queen},   // +1
	}

	if !hand.CanRedealForWeakHand(0) {
		t.Error("Player 0 should be eligible for redeal")
	}
	if hand.CanRedealForWeakHand(1) {
		t.Error("Player 1 should NOT be eligible for redeal")
	}
}

// TestHand_BeatsCard_Mighty tests Mighty card priority
// Rule: Mighty beats everything
func TestHand_BeatsCard_Mighty(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Contract = &Contract{
		Trump: Trump{Suit: Hearts, NoTrump: false},
	}

	mighty := Card{Suit: Spades, Rank: Ace}
	ace := Card{Suit: Hearts, Rank: Ace}

	trick := NewTrick(1, 0)
	trick.AddCard(0, ace)

	// Mighty should beat regular ace
	if !hand.BeatsCard(mighty, ace, trick, Hearts) {
		t.Error("Mighty should beat any card")
	}

	// Regular ace should not beat Mighty
	if hand.BeatsCard(ace, mighty, trick, Hearts) {
		t.Error("No card should beat Mighty")
	}
}

// TestHand_BeatsCard_Joker tests Joker priority
// Rule: Joker beats everything except Mighty (unless ripped)
func TestHand_BeatsCard_Joker(t *testing.T) {
	hand := NewHand(1, 0, 5)
	hand.Contract = &Contract{
		Trump: Trump{Suit: Hearts, NoTrump: false},
	}

	joker := Card{Suit: NoSuit, Rank: Joker}
	ace := Card{Suit: Hearts, Rank: Ace}
	mighty := Card{Suit: Spades, Rank: Ace}

	trick := NewTrick(1, 0)
	trick.AddCard(0, ace)

	// Joker should beat regular cards
	if !hand.BeatsCard(joker, ace, trick, Hearts) {
		t.Error("Joker should beat regular cards")
	}

	// Mighty should beat Joker
	if hand.BeatsCard(joker, mighty, trick, Hearts) {
		t.Error("Joker should not beat Mighty")
	}
}

// TestHand_BeatsCard_Trump tests trump priority
// Rule: Trump cards beat non-trump cards
func TestHand_BeatsCard_Trump(t *testing.T) {
	hand := NewHand(1, 0, 5)

	trumpCard := Card{Suit: Hearts, Rank: Two}
	nonTrumpAce := Card{Suit: Spades, Rank: Ace}

	trick := NewTrick(1, 0)
	trick.AddCard(0, nonTrumpAce)

	// Low trump should beat high non-trump
	if !hand.BeatsCard(trumpCard, nonTrumpAce, trick, Hearts) {
		t.Error("Trump card should beat non-trump card")
	}
}

// TestHand_BeatsCard_FollowSuit tests following suit
// Rule: Must follow lead suit; highest card of lead suit wins
func TestHand_BeatsCard_FollowSuit(t *testing.T) {
	hand := NewHand(1, 0, 5)

	leadCard := Card{Suit: Spades, Rank: Ten}
	higherSpade := Card{Suit: Spades, Rank: Ace}
	lowerSpade := Card{Suit: Spades, Rank: Five}
	differentSuit := Card{Suit: Hearts, Rank: Ace}

	trick := NewTrick(1, 0)
	trick.AddCard(0, leadCard)

	// Higher spade should beat lower spade
	if !hand.BeatsCard(higherSpade, lowerSpade, trick, Clubs) {
		t.Error("Higher card of same suit should win")
	}

	// Different suit should not beat lead suit
	if hand.BeatsCard(differentSuit, leadCard, trick, Clubs) {
		t.Error("Card not following suit should not win")
	}
}
