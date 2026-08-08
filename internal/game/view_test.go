package game

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// dealtGame returns a started five-player game: ten cards in every hand and
// three in the kitty.
func dealtGame(t *testing.T) *Game {
	t.Helper()

	g := New("g1")
	for i := range 5 {
		g.Players[i] = &Player{
			ID:          fmt.Sprintf("p%d", i),
			Name:        fmt.Sprintf("Player %d", i),
			Seat:        i,
			IsConnected: true,
		}
	}

	g.Start()

	return g
}

func TestViewForShowsOnlyTheViewersHand(t *testing.T) {
	t.Parallel()

	g := dealtGame(t)
	v := g.ViewFor("p2")

	if got := len(v.Players[2].Hand); got != 10 {
		t.Errorf("viewer should see their own 10 cards, got %d", got)
	}

	for i, pv := range v.Players {
		if i == 2 {
			continue
		}

		if pv.Hand != nil {
			t.Errorf("seat %d leaked a hand of %d cards", i, len(pv.Hand))
		}

		if pv.HandCount != 10 {
			t.Errorf("seat %d: want HandCount 10, got %d", i, pv.HandCount)
		}
	}
}

func TestViewForHidesTheKitty(t *testing.T) {
	t.Parallel()

	g := dealtGame(t)
	if len(g.Kitty) != 3 {
		t.Fatalf("fixture should have a 3-card kitty, got %d", len(g.Kitty))
	}

	if v := g.ViewFor("p0"); v.Kitty != nil {
		t.Errorf("kitty leaked: %v", v.Kitty)
	}
}

func TestViewForSpectatorSeesNoHands(t *testing.T) {
	t.Parallel()

	g := dealtGame(t)

	for _, userID := range []string{"", "not-a-player"} {
		v := g.ViewFor(userID)
		for i, pv := range v.Players {
			if pv.Hand != nil {
				t.Errorf("ViewFor(%q): seat %d leaked a hand", userID, i)
			}

			if pv.HandCount != 10 {
				t.Errorf("ViewFor(%q): seat %d: want HandCount 10, got %d", userID, i, pv.HandCount)
			}
		}
	}
}

func TestViewForDoesNotMutateTheSource(t *testing.T) {
	t.Parallel()

	g := dealtGame(t)
	_ = g.ViewFor("p1")

	if len(g.Kitty) != 3 {
		t.Errorf("ViewFor emptied the kitty: %d cards left", len(g.Kitty))
	}

	for i, p := range g.Players {
		if len(p.Hand) != 10 {
			t.Errorf("ViewFor damaged seat %d's hand: %d cards left", i, len(p.Hand))
		}
	}
}

func TestViewForKeepsEmptySeatsNil(t *testing.T) {
	t.Parallel()

	g := New("g1")
	g.Players[0] = &Player{ID: "p0", Name: "Player 0", Seat: 0, IsConnected: true}

	v := g.ViewFor("p0")
	if len(v.Players) != 5 {
		t.Fatalf("want 5 seats, got %d", len(v.Players))
	}

	if v.Players[0] == nil {
		t.Error("seat 0 should be populated")
	}

	for i := 1; i < 5; i++ {
		if v.Players[i] != nil {
			t.Errorf("empty seat %d should marshal as null, got %+v", i, v.Players[i])
		}
	}
}

func TestViewForMarshalsExactlyOneHand(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(dealtGame(t).ViewFor("p3"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// `"hand"` does not match `"hand_count"`: the character after `hand` is a
	// quote in one and an underscore in the other.
	if got := strings.Count(string(data), `"hand"`); got != 1 {
		t.Errorf("want exactly 1 \"hand\" key on the wire, got %d\n%s", got, data)
	}

	if strings.Contains(string(data), `"kitty"`) {
		t.Errorf("kitty reached the wire\n%s", data)
	}

	if !strings.Contains(string(data), `"hand_count":10`) {
		t.Errorf("hand_count missing from the wire\n%s", data)
	}
}

// gameFieldClassification records a redaction decision for every field on
// Game. GameView embeds *Game, so a newly added field reaches clients
// automatically. This map is the gate that forces the decision to be made
// knowingly rather than by default.
var gameFieldClassification = map[string]string{
	"ID": "public", "Status": "public", "Config": "public",
	"Players": "per-player", "Kitty": "secret", "Deck": "secret",
	"CurrentTurn": "public", "Dealer": "public", "Bids": "public",
	"CurrentBid": "public", "Declarer": "public", "PassedPlayers": "public",
	"Contract": "public", "PartnerCard": "public", "PartnerSeat": "public",
	"IsNoFriend": "public", "Trump": "public", "Tricks": "public",
	"Scores": "public", "TotalScores": "public", "ScoreHistory": "public",
	"PlayAgainVotes": "public", "Version": "public",
	"CreatedAt": "public", "UpdatedAt": "public",
}

// jsonTagName resolves the wire name encoding/json would use for f: the part
// of the json tag before the first comma (so `json:"foo,omitempty"` yields
// "foo"), or the Go field name when the tag is absent or empty, matching
// encoding/json's own fallback.
func jsonTagName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}

	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return f.Name
	}

	return name
}

func TestEveryGameFieldIsClassified(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(Game{})
	viewTyp := reflect.TypeOf(GameView{})

	for i := range typ.NumField() {
		field := typ.Field(i)
		name := field.Name

		class, ok := gameFieldClassification[name]
		if !ok {
			t.Errorf("game.Game field %q has no redaction classification. "+
				"Add it to gameFieldClassification in view_test.go, and if it is "+
				"secret or per-player, shadow it in GameView.", name)

			continue
		}

		if class != "secret" && class != "per-player" {
			continue
		}

		// A classification of secret or per-player is only honored if the
		// field is either excluded from JSON entirely or shadowed by a
		// depth-0 field on GameView with the same wire name: encoding/json
		// resolves depth-0 fields over the ones *Game promotes, so that's
		// what actually keeps the value off the wire. Without this check,
		// classifying a field is a no-op that the compiler and test both let
		// through silently.
		tagName := jsonTagName(field)
		if tagName == "-" {
			continue
		}

		shadowed := false

		for j := range viewTyp.NumField() {
			vf := viewTyp.Field(j)
			if vf.Anonymous {
				continue
			}

			if jsonTagName(vf) == tagName {
				shadowed = true
				break
			}
		}

		if !shadowed {
			t.Errorf("game.Game field %q is classified %q but GameView has no "+
				"depth-0 field shadowing its JSON key %q, and Game.%s is not "+
				"tagged `json:\"-\"`. Add a field to GameView with `json:\"%s\"` "+
				"that holds the redacted value (see Kitty), or tag Game.%s as "+
				"`json:\"-\"` if it must never be marshalled at all (see Deck).",
				name, class, tagName, name, tagName, name)
		}
	}

	if got, want := typ.NumField(), len(gameFieldClassification); got != want {
		t.Errorf("gameFieldClassification has %d entries for %d Game fields; remove stale entries", want, got)
	}
}
