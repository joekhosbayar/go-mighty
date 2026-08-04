package game

import "time"

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

// LobbySeatView is the per-seat projection exposed to the lobby listing. It
// declares no Hand and no Points field at all, unlike PlayerView, because a
// lobby row shows a table's occupancy - never one player's cards.
type LobbySeatView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Seat        int    `json:"seat"`
	HandCount   int    `json:"hand_count"`
	IsConnected bool   `json:"is_connected"`
}

// LobbyGameView is everything the lobby feed sends over the wire. It does
// not embed *Game: GameView's safety depends on shadowing depth-0 fields
// over ones *Game promotes (see TestEveryGameFieldIsClassified), which is a
// property you have to keep proving as Game grows. LobbyGameView instead has
// no field capable of holding a hand or the kitty, so
// LobbyEvent{Game: g.ViewFor(userID)}-style copy-paste mistakes have nowhere
// to put the leak even if someone tries.
type LobbyGameView struct {
	ID        string           `json:"id"`
	Status    Phase            `json:"status"`
	Config    GameConfig       `json:"config"`
	Players   []*LobbySeatView `json:"players"`
	Version   int64            `json:"version"`
	CreatedAt time.Time        `json:"created_at"`
}

// LobbyView projects g for the lobby listing: table identity, status, seat
// occupancy, and per-seat card counts - the fields mighty-frontend's
// LobbyScreen actually reads (id, created_at, config.num_players, and
// per-seat occupancy). No caller-supplied userID: a lobby row is the same
// for every viewer, so there is nothing to redact per-viewer, only fields to
// leave out entirely.
func (g *Game) LobbyView() *LobbyGameView {
	if g == nil {
		return nil
	}

	seats := make([]*LobbySeatView, len(g.Players))

	for i, p := range g.Players {
		if p == nil {
			continue
		}

		seats[i] = &LobbySeatView{
			ID:          p.ID,
			Name:        p.Name,
			Seat:        p.Seat,
			HandCount:   len(p.Hand),
			IsConnected: p.IsConnected,
		}
	}

	return &LobbyGameView{
		ID:        g.ID,
		Status:    g.Status,
		Config:    g.Config,
		Players:   seats,
		Version:   g.Version,
		CreatedAt: g.CreatedAt,
	}
}
