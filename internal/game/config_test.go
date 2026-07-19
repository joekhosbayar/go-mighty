package game

import "testing"

func TestDefaultConfigIsFivePlayer(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.NumPlayers != 5 || !cfg.AllowJokerPartner || cfg.FailDist != FailEqualSplit {
		t.Fatalf("unexpected default config: %+v", cfg)
	}
}

func TestNewDefaultsToFivePlayer(t *testing.T) {
	g := New("cfg")
	if g.numSeats() != 5 {
		t.Errorf("numSeats: got %d, want 5", g.numSeats())
	}
	if g.minBidPoints() != 3 {
		t.Errorf("minBidPoints: got %d, want 3", g.minBidPoints())
	}
}

func TestNewWithConfigFourPlayer(t *testing.T) {
	g := NewWithConfig("cfg4", GameConfig{NumPlayers: 4, AllowJokerPartner: false, FailDist: FailTwoOneSplit})
	if g.numSeats() != 4 {
		t.Errorf("numSeats: got %d, want 4", g.numSeats())
	}
	if g.minBidPoints() != 4 {
		t.Errorf("minBidPoints: got %d, want 4", g.minBidPoints())
	}
}
