package service

import "github.com/joekhosbayar/go-mighty/internal/game"

// Logical event names published to Redis.
const (
	EventTypeMove         = "move"
	EventTypePlayerJoined = "player_joined"
	EventTypeGameCreated  = "game_created"
	EventTypeGameJoined   = "game_joined"
)

// GameEvent is the envelope published to a game's Redis channel. It carries
// full state on purpose: Redis is internal, and the socket edge is what
// redacts. Every field a client may see must be declared here, because the
// WebSocket relay decodes into this type and re-encodes - anything undeclared
// is dropped rather than forwarded.
type GameEvent struct {
	Type      string        `json:"type"`
	MoveType  game.MoveType `json:"move_type,omitempty"`
	PlayerID  string        `json:"player_id,omitempty"`
	Seat      *int          `json:"seat,omitempty"`
	Version   int64         `json:"version"`
	GameState *game.Game    `json:"game_state,omitempty"`
}

// EventType lets the Redis store log the logical name rather than the Go type.
func (e GameEvent) EventType() string { return e.Type }

// OutgoingGameEvent is what actually reaches a client socket.
type OutgoingGameEvent struct {
	Type      string         `json:"type"`
	MoveType  game.MoveType  `json:"move_type,omitempty"`
	PlayerID  string         `json:"player_id,omitempty"`
	Seat      *int           `json:"seat,omitempty"`
	Version   int64          `json:"version"`
	GameState *game.GameView `json:"game_state,omitempty"`
}

// RedactFor projects the event for a single viewer.
func (e GameEvent) RedactFor(userID string) OutgoingGameEvent {
	out := OutgoingGameEvent{
		Type:     e.Type,
		MoveType: e.MoveType,
		PlayerID: e.PlayerID,
		Seat:     e.Seat,
		Version:  e.Version,
	}

	if e.GameState != nil {
		out.GameState = e.GameState.ViewFor(userID)
	}

	return out
}

// LobbyEvent is the envelope published to the lobby channel. Game is a
// LobbyGameView, not a GameView: GameView still carries the *Game embed
// (its safety comes from shadowing, not absence), so putting one here would
// let a future publisher pass a per-player ViewFor(userID) result straight
// through and fan one player's hand out to every socket in the lobby.
// LobbyGameView has no field able to hold a hand or the kitty regardless of
// which viewer built it, which is what makes this relay fail-closed by
// construction rather than by the relay decoding-and-re-encoding discipline
// alone (see lobby_ws.go).
type LobbyEvent struct {
	Type          string              `json:"type"`
	GameID        string              `json:"game_id,omitempty"`
	Game          *game.LobbyGameView `json:"game,omitempty"`
	PlayersSeated int                 `json:"players_seated,omitempty"`
	MaxPlayers    int                 `json:"max_players,omitempty"`
}

// EventType lets the Redis store log the logical name rather than the Go type.
func (e LobbyEvent) EventType() string { return e.Type }
