package game

import "testing"

func TestFourPlayerStartDealsFourHands(t *testing.T) {
	g := NewWithConfig("deal4", GameConfig{NumPlayers: 4, AllowJokerPartner: true, FailDist: FailEqualSplit})
	for i := 0; i < 4; i++ {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i}
	}
	if !g.IsFull() {
		t.Fatal("four seated players should fill a four-player game")
	}
	g.Start()
	for i := 0; i < 4; i++ {
		if got := len(g.Players[i].Hand); got != 10 {
			t.Errorf("seat %d hand: got %d, want 10", i, got)
		}
	}
	if g.Players[4] != nil {
		t.Error("seat 4 must stay nil in a four-player game")
	}
	if len(g.Kitty) != 3 {
		t.Errorf("kitty: got %d, want 3", len(g.Kitty))
	}
	if g.Status != PhaseBidding {
		t.Errorf("status: got %s, want %s", g.Status, PhaseBidding)
	}
}

func TestFivePlayerIsFullStillFive(t *testing.T) {
	g := New("full5")
	for i := 0; i < 4; i++ {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i}
	}
	if g.IsFull() {
		t.Fatal("four of five seats should not be full")
	}
}
