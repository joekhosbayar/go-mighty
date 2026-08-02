package game

// PlayerView is the per-viewer projection of a Player. Its fields are listed
// explicitly rather than embedded because this is where every secret lives: a
// field must be named here to reach a client.
type PlayerView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Seat        int    `json:"seat"`
	Hand        []Card `json:"hand,omitempty"` // populated only for the viewer
	HandCount   int    `json:"hand_count"`     // populated for every seat
	Points      []Card `json:"points,omitempty"`
	IsConnected bool   `json:"is_connected"`
}

// GameView embeds *Game so public fields promote automatically, then shadows
// the two that carry secrets. encoding/json resolves depth-0 fields over
// promoted ones, so the Players and Kitty declared here are what reach the
// wire. TestEveryGameFieldIsClassified guards the consequence: a field added
// to Game becomes visible to clients unless someone shadows it here.
type GameView struct {
	*Game

	Players []*PlayerView `json:"players"`
	Kitty   []Card        `json:"kitty,omitempty"` // always nil
}

// ViewFor projects g for the client identified by userID. Any ID matching no
// seat - including the empty string - yields the public view: card counts and
// no hands, which is what spectators get. Players and spectators run through
// one path so the two cannot drift apart.
//
// The receiver is never mutated: the Players slice is rebuilt and everything
// else is aliased read-only, so game rules keep operating on full state.
func (g *Game) ViewFor(userID string) *GameView {
	if g == nil {
		return nil
	}

	views := make([]*PlayerView, len(g.Players))

	for i, p := range g.Players {
		if p == nil {
			continue
		}

		pv := &PlayerView{
			ID:          p.ID,
			Name:        p.Name,
			Seat:        p.Seat,
			HandCount:   len(p.Hand),
			Points:      p.Points,
			IsConnected: p.IsConnected,
		}

		if userID != "" && p.ID == userID {
			pv.Hand = p.Hand
		}

		views[i] = pv
	}

	return &GameView{Game: g, Players: views}
}
