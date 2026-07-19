package game

import "testing"

func TestNewDeckForFourPlayerHas43Cards(t *testing.T) {
	d := NewDeckFor(4)
	if len(d) != 43 {
		t.Fatalf("four-player deck size: got %d, want 43", len(d))
	}
	removed := map[Card]bool{
		{Hearts, Three}: true, {Diamonds, Three}: true,
	}
	pointCards := 0
	for _, c := range d {
		if c.Rank == Two || c.Rank == Four {
			t.Errorf("2s and 4s must be removed, found %s", c)
		}
		if removed[c] {
			t.Errorf("red 3 must be removed, found %s", c)
		}
		if c.IsPointCard() {
			pointCards++
		}
	}
	if pointCards != 20 {
		t.Errorf("point cards: got %d, want 20", pointCards)
	}
	// Black 3s stay.
	hasBlackThrees := 0
	for _, c := range d {
		if c.Rank == Three && (c.Suit == Spades || c.Suit == Clubs) {
			hasBlackThrees++
		}
	}
	if hasBlackThrees != 2 {
		t.Errorf("black 3s: got %d, want 2", hasBlackThrees)
	}
}

func TestNewDeckForFivePlayerHas53Cards(t *testing.T) {
	if len(NewDeckFor(5)) != 53 {
		t.Fatalf("five-player deck size: got %d, want 53", len(NewDeckFor(5)))
	}
}

func TestDealFourPlayer(t *testing.T) {
	d := NewDeckFor(4)
	hands, kitty := d.Deal(4)
	if len(hands) != 4 {
		t.Fatalf("hands: got %d, want 4", len(hands))
	}
	for i, h := range hands {
		if len(h) != 10 {
			t.Errorf("hand %d: got %d cards, want 10", i, len(h))
		}
	}
	if len(kitty) != 3 {
		t.Errorf("kitty: got %d, want 3", len(kitty))
	}
}

func TestDealFivePlayer(t *testing.T) {
	hands, kitty := NewDeckFor(5).Deal(5)
	if len(hands) != 5 || len(kitty) != 3 {
		t.Fatalf("five-player deal shape wrong: %d hands, %d kitty", len(hands), len(kitty))
	}
}
