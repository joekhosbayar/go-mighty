package game

import (
	"fmt"
	"testing"
)

// fourScoringGame builds a finished four-player hand. Seat 0 is declarer; seat 1
// holds the called partner card unless noFriend. Seats 0-3 filled; seat 4 nil.
func fourScoringGame(bid int, fd FailDist, noFriend bool, teamPoints int) *Game {
	g := NewWithConfig("score4", GameConfig{NumPlayers: 4, AllowJokerPartner: true, FailDist: fd})
	g.Declarer = 0
	g.Contract = &Bid{Points: bid, Suit: Spades}
	for i := 0; i < 4; i++ {
		g.Players[i] = &Player{ID: fmt.Sprintf("p%d", i), Seat: i}
	}
	if noFriend {
		g.IsNoFriend = true
	} else {
		g.PartnerCard = &Card{Suit: Hearts, Rank: King}
		g.Players[1].Hand = []Card{{Suit: Hearts, Rank: King}}
	}
	g.Players[0].Points = make([]Card, teamPoints)
	return g
}

func TestFourPlayerScore(t *testing.T) {
	tests := []struct {
		name                             string
		bid                              int
		fd                               FailDist
		noFriend                         bool
		teamPoints                       int
		wantDecl, wantPartner, wantOppEa int
	}{
		// success: s = 2*(5-4) + (16-15) = 3; win split declarer +3 partner +3 opp -3
		{"success 15s 16pts", 5, FailEqualSplit, false, 16, 3, 3, -3},
		// fail equal_split: s = 15-13 = 2; declarer -2 partner -2 opp +2
		{"fail equal 13pts", 5, FailEqualSplit, false, 13, -2, -2, 2},
		// fail declarer_alone: s=2; declarer -4 partner 0 opp +2
		{"fail declarer-alone 13pts", 5, FailDeclarerAlone, false, 13, -4, 0, 2},
		// fail two_one even: s=2; opp +3, partner -2, declarer -4
		{"fail two-one even 13pts", 5, FailTwoOneSplit, false, 13, -4, -2, 3},
		// fail two_one odd: s = 15-12 = 3; opp +5, partner -3, declarer -7
		{"fail two-one odd 12pts", 5, FailTwoOneSplit, false, 12, -7, -3, 5},
		// alone success: s=3, doubled to 6 by IsNoFriend; declarer +18 vs three opponents -6
		{"alone success 16pts", 5, FailEqualSplit, true, 16, 18, 0, -6},
		// alone fail: s=2, doubled to 4 by IsNoFriend; declarer -12, opp +4 (fd ignored when alone)
		{"alone fail 13pts", 5, FailTwoOneSplit, true, 13, -12, 0, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := fourScoringGame(tc.bid, tc.fd, tc.noFriend, tc.teamPoints)
			got := g.CalculateFinalScore()
			if got[0] != tc.wantDecl {
				t.Errorf("declarer: got %d, want %d", got[0], tc.wantDecl)
			}
			if !tc.noFriend && got[1] != tc.wantPartner {
				t.Errorf("partner: got %d, want %d", got[1], tc.wantPartner)
			}
			// Opponents: seats 2,3 always; seat 1 too when alone.
			oppSeats := []int{2, 3}
			if tc.noFriend {
				oppSeats = []int{1, 2, 3}
			}
			for _, s := range oppSeats {
				if got[s] != tc.wantOppEa {
					t.Errorf("opp seat %d: got %d, want %d", s, got[s], tc.wantOppEa)
				}
			}
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
