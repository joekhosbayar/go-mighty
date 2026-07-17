package game

import (
	"errors"
	"fmt"
	"testing"
)

// callingGame returns a 5-player game in the calling phase with seat 0 as declarer.
func callingGame() *Game {
	g := New("friend-test")
	for i := range 5 {
		g.Players[i] = &Player{ID: fmt.Sprintf("p%d", i), Name: fmt.Sprintf("P%d", i), Seat: i, Hand: []Card{}, Points: []Card{}}
	}

	g.Status = PhaseCalling
	g.Declarer = 0
	g.CurrentTurn = 0
	g.Trump = Spades
	g.Contract = &Bid{PlayerID: "p0", Points: 7, Suit: Spades}

	return g
}

func TestCallPartnerWithCardSetsPartnerCard(t *testing.T) {
	t.Parallel()

	g := callingGame()
	move := CallPartnerMove{Card: &Card{Suit: Diamonds, Rank: Ace}}

	if err := g.ValidateMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if err := g.ApplyMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerCard == nil || g.PartnerCard.Rank != Ace || g.PartnerCard.Suit != Diamonds {
		t.Fatalf("partner card not stored: %+v", g.PartnerCard)
	}

	if g.IsNoFriend {
		t.Fatal("IsNoFriend must stay false when a card is called")
	}

	if g.Status != PhasePlaying {
		t.Fatalf("expected playing, got %s", g.Status)
	}
}

func TestCallPartnerNoFriendSetsFlagAndSkipsCard(t *testing.T) {
	t.Parallel()

	g := callingGame()
	move := CallPartnerMove{NoFriend: true}

	if err := g.ValidateMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if err := g.ApplyMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if !g.IsNoFriend {
		t.Fatal("IsNoFriend not set")
	}

	if g.PartnerCard != nil {
		t.Fatalf("partner card must be nil, got %+v", g.PartnerCard)
	}

	if g.Status != PhasePlaying {
		t.Fatalf("expected playing, got %s", g.Status)
	}
}

func TestCallPartnerRejectsBothAndNeither(t *testing.T) {
	t.Parallel()

	both := CallPartnerMove{Card: &Card{Suit: Hearts, Rank: Ace}, NoFriend: true}
	if err := callingGame().ValidateMove("p0", MoveCallPartner, both); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("both card and no_friend must be rejected, got %v", err)
	}

	neither := CallPartnerMove{}
	if err := callingGame().ValidateMove("p0", MoveCallPartner, neither); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("empty call must be rejected, got %v", err)
	}
}

func TestCallPartnerLegacyBareCardStillAccepted(t *testing.T) {
	t.Parallel()

	g := callingGame()
	card := Card{Suit: Hearts, Rank: Ace}

	if err := g.ValidateMove("p0", MoveCallPartner, card); err != nil {
		t.Fatalf("validate legacy card: %v", err)
	}

	if err := g.ApplyMove("p0", MoveCallPartner, card); err != nil {
		t.Fatalf("apply legacy card: %v", err)
	}

	if g.PartnerCard == nil || g.PartnerCard.Suit != Hearts {
		t.Fatalf("legacy card not stored: %+v", g.PartnerCard)
	}
}

// playingGameWithPartnerCard returns a game mid-play (trick 2 open) where the
// declarer (seat 0) has called ♦A and it sits in seat 2's hand.
func playingGameWithPartnerCard() *Game {
	g := callingGame()
	g.Status = PhasePlaying
	g.PartnerCard = &Card{Suit: Diamonds, Rank: Ace}
	g.Tricks = []Trick{
		{Cards: make([]PlayedCard, 5), LeadSuit: Clubs, Winner: 2},
		{Cards: []PlayedCard{}},
	}
	g.CurrentTurn = 2
	g.Players[2].Hand = []Card{{Suit: Diamonds, Rank: Ace}, {Suit: Clubs, Rank: Two}}

	return g
}

func TestPlayingCalledCardRevealsPartner(t *testing.T) {
	t.Parallel()

	g := playingGameWithPartnerCard()
	move := PlayCardMove{Card: Card{Suit: Diamonds, Rank: Ace}}

	if err := g.ValidateMove("p2", MovePlayCard, move); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if err := g.ApplyMove("p2", MovePlayCard, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != 2 {
		t.Fatalf("expected partner seat 2, got %d", g.PartnerSeat)
	}
}

func TestDeclarerPlayingOwnCalledCardIsSelfPartner(t *testing.T) {
	t.Parallel()

	g := playingGameWithPartnerCard()
	g.CurrentTurn = 0
	g.Players[0].Hand = []Card{{Suit: Diamonds, Rank: Ace}}
	g.Players[2].Hand = []Card{{Suit: Clubs, Rank: Two}}

	move := PlayCardMove{Card: Card{Suit: Diamonds, Rank: Ace}}
	if err := g.ApplyMove("p0", MovePlayCard, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != 0 {
		t.Fatalf("expected self-partner seat 0, got %d", g.PartnerSeat)
	}
}

func TestUnplayedCalledCardScoresDeclarerAloneWithoutDoubling(t *testing.T) {
	t.Parallel()

	g := callingGame()
	g.PartnerCard = &Card{Suit: Diamonds, Rank: Ace} // stayed in the kitty
	g.PartnerSeat = -1
	g.IsNoFriend = false
	// Declarer alone wins exactly the 7-trick contract.
	g.Tricks = make([]Trick, 10)
	for i := range g.Tricks {
		if i < 7 {
			g.Tricks[i].Winner = 0
		} else {
			g.Tricks[i].Winner = 3
		}
	}

	declarer, friend := g.CalculateFinalScore()
	if declarer != 70 {
		t.Fatalf("expected 70 (no x2 doubling), got %v", declarer)
	}

	if friend != 0 {
		t.Fatalf("expected friend score 0 with no revealed partner, got %v", friend)
	}
}

func TestScoringCountsRevealedPartnerTricks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		contract          int
		declarerTricks    int
		partnerTricks     int
		noTrump, noFriend bool
		wantDeclarer      float64
		wantFriend        float64
	}{
		{name: "exact contract split", contract: 7, declarerTricks: 4, partnerTricks: 3, wantDeclarer: 70, wantFriend: 35},
		{name: "overtricks", contract: 7, declarerTricks: 5, partnerTricks: 4, wantDeclarer: 80, wantFriend: 40},
		{name: "down one", contract: 7, declarerTricks: 4, partnerTricks: 2, wantDeclarer: -70, wantFriend: -35},
		{name: "down two adds penalty", contract: 7, declarerTricks: 3, partnerTricks: 2, wantDeclarer: -75, wantFriend: -37.5},
		{name: "no trump doubles", contract: 7, declarerTricks: 4, partnerTricks: 3, noTrump: true, wantDeclarer: 140, wantFriend: 70},
		{name: "ten bid doubles", contract: 10, declarerTricks: 6, partnerTricks: 4, wantDeclarer: 200, wantFriend: 100},
		{name: "cap at 800", contract: 10, declarerTricks: 10, partnerTricks: 0, noTrump: true, noFriend: true, wantDeclarer: 800, wantFriend: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g := callingGame()
			g.Contract = &Bid{PlayerID: "p0", Points: tc.contract, Suit: Spades, IsNoTrump: tc.noTrump}
			if tc.noTrump {
				g.Contract.Suit = None
			}

			g.IsNoFriend = tc.noFriend
			g.PartnerSeat = 1
			if tc.noFriend {
				g.PartnerSeat = -1
			}

			g.Tricks = make([]Trick, 10)
			seat := 0

			for i := range g.Tricks {
				switch {
				case seat < tc.declarerTricks:
					g.Tricks[i].Winner = 0
				case seat < tc.declarerTricks+tc.partnerTricks:
					g.Tricks[i].Winner = 1
				default:
					g.Tricks[i].Winner = 3
				}
				seat++
			}

			declarer, friend := g.CalculateFinalScore()
			if declarer != tc.wantDeclarer || friend != tc.wantFriend {
				t.Fatalf("got (%v, %v), want (%v, %v)", declarer, friend, tc.wantDeclarer, tc.wantFriend)
			}
		})
	}
}
