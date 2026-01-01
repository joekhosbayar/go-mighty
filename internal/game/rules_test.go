package game

import (
	"testing"
)

// TestBid_IsHigherThan tests bid comparison logic
// Rule: Higher points beat lower points; no-trump beats same points with a suit
func TestBid_IsHigherThan(t *testing.T) {
	tests := []struct {
		name     string
		bid1     Bid
		bid2     Bid
		expected bool
	}{
		{
			name:     "Higher points should beat lower points",
			bid1:     NewBid(0, 15, Trump{Suit: Hearts, NoTrump: false}),
			bid2:     NewBid(1, 14, Trump{Suit: Spades, NoTrump: false}),
			expected: true,
		},
		{
			name:     "Lower points should not beat higher points",
			bid1:     NewBid(0, 13, Trump{Suit: Hearts, NoTrump: false}),
			bid2:     NewBid(1, 14, Trump{Suit: Spades, NoTrump: false}),
			expected: false,
		},
		{
			name:     "No-trump should beat same points with suit",
			bid1:     NewBid(0, 14, Trump{Suit: "", NoTrump: true}),
			bid2:     NewBid(1, 14, Trump{Suit: Hearts, NoTrump: false}),
			expected: true,
		},
		{
			name:     "Suit should not beat no-trump at same points",
			bid1:     NewBid(0, 14, Trump{Suit: Hearts, NoTrump: false}),
			bid2:     NewBid(1, 14, Trump{Suit: "", NoTrump: true}),
			expected: false,
		},
		{
			name:     "Same points same type should not beat (first bid wins)",
			bid1:     NewBid(0, 14, Trump{Suit: Hearts, NoTrump: false}),
			bid2:     NewBid(1, 14, Trump{Suit: Spades, NoTrump: false}),
			expected: false,
		},
		{
			name:     "Pass should never beat a real bid",
			bid1:     NewPass(0),
			bid2:     NewBid(1, 13, Trump{Suit: Hearts, NoTrump: false}),
			expected: false,
		},
		{
			name:     "Real bid should always beat a pass",
			bid1:     NewBid(0, 13, Trump{Suit: Hearts, NoTrump: false}),
			bid2:     NewPass(1),
			expected: true,
		},
		{
			name:     "Pass should not beat another pass",
			bid1:     NewPass(0),
			bid2:     NewPass(1),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.bid1.IsHigherThan(tt.bid2)
			if result != tt.expected {
				t.Errorf("Bid.IsHigherThan() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestNewBid tests bid creation
// Rule: Bids should correctly store seat, points, and trump
func TestNewBid(t *testing.T) {
	bid := NewBid(2, 15, Trump{Suit: Hearts, NoTrump: false})

	if bid.SeatNo != 2 {
		t.Errorf("NewBid SeatNo = %d, want 2", bid.SeatNo)
	}
	if bid.Points != 15 {
		t.Errorf("NewBid Points = %d, want 15", bid.Points)
	}
	if bid.Trump.Suit != Hearts {
		t.Errorf("NewBid Trump.Suit = %v, want Hearts", bid.Trump.Suit)
	}
	if bid.Passed {
		t.Error("NewBid should not be marked as passed")
	}
}

// TestNewPass tests pass creation
// Rule: Pass should mark bid as passed
func TestNewPass(t *testing.T) {
	pass := NewPass(3)

	if pass.SeatNo != 3 {
		t.Errorf("NewPass SeatNo = %d, want 3", pass.SeatNo)
	}
	if !pass.Passed {
		t.Error("NewPass should be marked as passed")
	}
}

// TestTrump_NoTrump tests no-trump representation
// Rule: No-trump should be indicated by NoTrump flag
func TestTrump_NoTrump(t *testing.T) {
	noTrump := Trump{Suit: "", NoTrump: true}

	if !noTrump.NoTrump {
		t.Error("No-trump should have NoTrump=true")
	}

	withTrump := Trump{Suit: Spades, NoTrump: false}

	if withTrump.NoTrump {
		t.Error("Trump with suit should have NoTrump=false")
	}
}

// TestTrick_NewTrick tests trick creation
// Rule: New trick should initialize with correct trick number and leader
func TestTrick_NewTrick(t *testing.T) {
	trick := NewTrick(5, 2)

	if trick.TrickNo != 5 {
		t.Errorf("NewTrick TrickNo = %d, want 5", trick.TrickNo)
	}
	if trick.LeaderSeat != 2 {
		t.Errorf("NewTrick LeaderSeat = %d, want 2", trick.LeaderSeat)
	}
	if trick.WinnerSeat != -1 {
		t.Errorf("NewTrick WinnerSeat = %d, want -1 (unset)", trick.WinnerSeat)
	}
	if trick.Points != 0 {
		t.Errorf("NewTrick Points = %d, want 0", trick.Points)
	}
	if len(trick.Cards) != 0 {
		t.Errorf("NewTrick should have 0 cards, has %d", len(trick.Cards))
	}
}

// TestTrick_AddCard tests adding cards to a trick
// Rule: Cards should be added in order with correct seat and point accumulation
func TestTrick_AddCard(t *testing.T) {
	trick := NewTrick(1, 0)

	// Add Ace (1 point)
	trick.AddCard(0, Card{Suit: Spades, Rank: Ace})

	if len(trick.Cards) != 1 {
		t.Errorf("After adding 1 card, trick has %d cards", len(trick.Cards))
	}
	if trick.Points != 1 {
		t.Errorf("After adding Ace, trick.Points = %d, want 1", trick.Points)
	}
	if trick.Cards[0].SeatNo != 0 {
		t.Errorf("First card SeatNo = %d, want 0", trick.Cards[0].SeatNo)
	}

	// Add King (1 point)
	trick.AddCard(1, Card{Suit: Hearts, Rank: King})

	if len(trick.Cards) != 2 {
		t.Errorf("After adding 2 cards, trick has %d cards", len(trick.Cards))
	}
	if trick.Points != 2 {
		t.Errorf("After adding Ace and King, trick.Points = %d, want 2", trick.Points)
	}

	// Add a card with no points
	trick.AddCard(2, Card{Suit: Clubs, Rank: Two})

	if trick.Points != 2 {
		t.Errorf("After adding non-point card, trick.Points = %d, want 2", trick.Points)
	}
}

// TestTrick_IsComplete tests trick completion detection
// Rule: Trick is complete when all players have played
func TestTrick_IsComplete(t *testing.T) {
	trick := NewTrick(1, 0)

	// Not complete with 0 cards
	if trick.IsComplete(5) {
		t.Error("Empty trick should not be complete")
	}

	// Add cards
	for i := 0; i < 4; i++ {
		trick.AddCard(i, Card{Suit: Spades, Rank: Ace})
		if trick.IsComplete(5) {
			t.Errorf("Trick with %d cards should not be complete for 5 players", i+1)
		}
	}

	// Add 5th card
	trick.AddCard(4, Card{Suit: Spades, Rank: Ace})

	// Now should be complete
	if !trick.IsComplete(5) {
		t.Error("Trick with 5 cards should be complete for 5 players")
	}
}

// TestTrick_LeadSuit tests lead suit detection
// Rule: Lead suit is the suit of the first card played
func TestTrick_LeadSuit(t *testing.T) {
	trick := NewTrick(1, 0)

	// No lead suit on empty trick
	leadSuit := trick.LeadSuit()
	if leadSuit != nil {
		t.Error("Empty trick should have nil lead suit")
	}

	// Add first card
	trick.AddCard(0, Card{Suit: Hearts, Rank: Ace})

	leadSuit = trick.LeadSuit()
	if leadSuit == nil {
		t.Fatal("Trick with cards should have lead suit")
	}
	if *leadSuit != Hearts {
		t.Errorf("Lead suit = %v, want Hearts", *leadSuit)
	}

	// Add second card of different suit - lead suit shouldn't change
	trick.AddCard(1, Card{Suit: Spades, Rank: King})

	leadSuit = trick.LeadSuit()
	if leadSuit == nil {
		t.Fatal("Trick with cards should have lead suit")
	}
	if *leadSuit != Hearts {
		t.Errorf("Lead suit = %v, want Hearts (should not change)", *leadSuit)
	}
}

// TestTrick_LeadSuit_Joker tests lead suit with Joker
// Rule: Joker has NoSuit, which should be the lead suit if played first
func TestTrick_LeadSuit_Joker(t *testing.T) {
	trick := NewTrick(1, 0)

	// Lead with Joker
	trick.AddCard(0, Card{Suit: NoSuit, Rank: Joker})

	leadSuit := trick.LeadSuit()
	if leadSuit == nil {
		t.Fatal("Trick with Joker should have lead suit")
	}
	if *leadSuit != NoSuit {
		t.Errorf("Lead suit with Joker = %v, want NoSuit", *leadSuit)
	}
}

// TestPartnerCall_Types tests different partner call types
// Rule: Partner can be called by card, first trick winner, or no friend
func TestPartnerCall_Types(t *testing.T) {
	// Call by card
	callCard := Card{Suit: Spades, Rank: Ace}
	call1 := PartnerCall{
		Type: PartnerCallCard,
		Card: &callCard,
	}

	if call1.Type != PartnerCallCard {
		t.Error("PartnerCall type should be PartnerCallCard")
	}
	if call1.Card == nil {
		t.Fatal("PartnerCall.Card should not be nil")
	}
	if !call1.Card.Equals(callCard) {
		t.Error("PartnerCall.Card does not match expected card")
	}

	// First trick winner
	call2 := PartnerCall{
		Type: PartnerCallFirstTrick,
	}

	if call2.Type != PartnerCallFirstTrick {
		t.Error("PartnerCall type should be PartnerCallFirstTrick")
	}

	// No friend
	call3 := PartnerCall{
		Type: PartnerCallNoFriend,
	}

	if call3.Type != PartnerCallNoFriend {
		t.Error("PartnerCall type should be PartnerCallNoFriend")
	}
}

// TestPartnerCall_WithLeadSuit tests 20 no-trump with lead suit request
// Rule: For 20 no-trump, declarer may request specific lead from partner
func TestPartnerCall_WithLeadSuit(t *testing.T) {
	callCard := Card{Suit: Diamonds, Rank: Jack}
	leadSuit := Hearts

	call := PartnerCall{
		Type:     PartnerCallCard,
		Card:     &callCard,
		LeadSuit: &leadSuit,
	}

	if call.LeadSuit == nil {
		t.Fatal("LeadSuit should not be nil")
	}
	if *call.LeadSuit != Hearts {
		t.Errorf("LeadSuit = %v, want Hearts", *call.LeadSuit)
	}
}

// TestContract_Creation tests contract structure
// Rule: Contract should store declarer, points, trump, and partner info
func TestContract_Creation(t *testing.T) {
	contract := Contract{
		DeclarerSeat: 2,
		Points:       16,
		Trump:        Trump{Suit: Spades, NoTrump: false},
		NoFriend:     false,
	}

	if contract.DeclarerSeat != 2 {
		t.Errorf("Contract.DeclarerSeat = %d, want 2", contract.DeclarerSeat)
	}
	if contract.Points != 16 {
		t.Errorf("Contract.Points = %d, want 16", contract.Points)
	}
	if contract.Trump.Suit != Spades {
		t.Errorf("Contract.Trump.Suit = %v, want Spades", contract.Trump.Suit)
	}
	if contract.NoFriend {
		t.Error("Contract.NoFriend should be false")
	}
}

// TestContract_NoFriend tests no-friend contract
// Rule: Declarer can play alone for doubled score
func TestContract_NoFriend(t *testing.T) {
	contract := Contract{
		DeclarerSeat: 1,
		Points:       18,
		Trump:        Trump{Suit: Hearts, NoTrump: false},
		NoFriend:     true,
	}

	if !contract.NoFriend {
		t.Error("Contract.NoFriend should be true")
	}
}

// TestGamePhase_Values tests all game phase constants
// Rule: Game progresses through defined phases
func TestGamePhase_Values(t *testing.T) {
	phases := []GamePhase{
		PhaseWaiting,
		PhaseBidding,
		PhaseKitty,
		PhaseDiscard,
		PhaseCallingPartner,
		PhasePlaying,
		PhaseHandComplete,
		PhaseGameComplete,
	}

	// Just verify they're all distinct
	phaseMap := make(map[GamePhase]bool)
	for _, phase := range phases {
		if phaseMap[phase] {
			t.Errorf("Duplicate phase value: %v", phase)
		}
		phaseMap[phase] = true
	}

	if len(phaseMap) != len(phases) {
		t.Errorf("Expected %d unique phases, got %d", len(phases), len(phaseMap))
	}
}

// TestPlayerRole_Values tests all player role constants
// Rule: Players have roles: declarer, partner, opponent, or undecided
func TestPlayerRole_Values(t *testing.T) {
	roles := []PlayerRole{
		RoleUndecided,
		RoleDeclarer,
		RolePartner,
		RoleOpponent,
	}

	// Verify they're all distinct
	roleMap := make(map[PlayerRole]bool)
	for _, role := range roles {
		if roleMap[role] {
			t.Errorf("Duplicate role value: %v", role)
		}
		roleMap[role] = true
	}

	if len(roleMap) != len(roles) {
		t.Errorf("Expected %d unique roles, got %d", len(roles), len(roleMap))
	}
}

// TestBid_MinMaxPoints tests bid point range validation
// Rule: Minimum bid is 13, maximum bid is 20
func TestBid_MinMaxPoints(t *testing.T) {
	minBid := NewBid(0, 13, Trump{Suit: Hearts, NoTrump: false})
	maxBid := NewBid(0, 20, Trump{Suit: Spades, NoTrump: false})

	if minBid.Points != 13 {
		t.Errorf("Minimum bid points = %d, want 13", minBid.Points)
	}
	if maxBid.Points != 20 {
		t.Errorf("Maximum bid points = %d, want 20", maxBid.Points)
	}

	// Test that max beats min
	if !maxBid.IsHigherThan(minBid) {
		t.Error("20 points should beat 13 points")
	}
}

// TestCardPlay_Structure tests CardPlay structure
// Rule: CardPlay should associate a card with the player who played it
func TestCardPlay_Structure(t *testing.T) {
	card := Card{Suit: Diamonds, Rank: Queen}
	play := CardPlay{
		SeatNo: 3,
		Card:   card,
	}

	if play.SeatNo != 3 {
		t.Errorf("CardPlay.SeatNo = %d, want 3", play.SeatNo)
	}
	if !play.Card.Equals(card) {
		t.Errorf("CardPlay.Card = %v, want %v", play.Card, card)
	}
}

// TestTrick_PointAccumulation tests point calculation in tricks
// Rule: Trick points should equal sum of point values of all cards
func TestTrick_PointAccumulation(t *testing.T) {
	trick := NewTrick(1, 0)

	// Add 3 point cards and 2 non-point cards
	trick.AddCard(0, Card{Suit: Spades, Rank: Ace})   // 1 point
	trick.AddCard(1, Card{Suit: Hearts, Rank: King})  // 1 point
	trick.AddCard(2, Card{Suit: Clubs, Rank: Two})    // 0 points
	trick.AddCard(3, Card{Suit: Diamonds, Rank: Ten}) // 1 point
	trick.AddCard(4, Card{Suit: Spades, Rank: Five})  // 0 points

	if trick.Points != 3 {
		t.Errorf("Trick.Points = %d, want 3", trick.Points)
	}
}
