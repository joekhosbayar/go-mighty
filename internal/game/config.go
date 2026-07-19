package game

// FailDist selects how losses are split when a four-player 2-vs-2 contract fails.
type FailDist string

const (
	// FailEqualSplit: declarer -S, partner -S, each opponent +S.
	FailEqualSplit FailDist = "equal_split"
	// FailDeclarerAlone: declarer -2S, partner 0, each opponent +S.
	FailDeclarerAlone FailDist = "declarer_alone"
	// FailTwoOneSplit: partner -S, each opponent +ceil(1.5S); declarer pays the
	// remainder (-2S, or -(2S+1) when S is odd so the amounts stay integers).
	FailTwoOneSplit FailDist = "two_one_split"
)

// GameConfig captures every difference between the four- and five-player games.
type GameConfig struct {
	NumPlayers        int      `json:"num_players"`
	AllowJokerPartner bool     `json:"allow_joker_partner"`
	FailDist          FailDist `json:"fail_dist"`
}

// DefaultConfig returns the standard five-player configuration.
func DefaultConfig() GameConfig {
	return GameConfig{NumPlayers: 5, AllowJokerPartner: true, FailDist: FailEqualSplit}
}

// numSeats is the number of players this game seats (4 or 5).
func (g *Game) numSeats() int {
	if g.Config.NumPlayers == 0 {
		return 5
	}
	return g.Config.NumPlayers
}

// NumSeatsPublic exposes the seat count to other packages.
func (g *Game) NumSeatsPublic() int { return g.numSeats() }

// minBidPoints is the lowest legal bid on the 3-10 scale: 3 (target 13) for
// five players, 4 (target 14) for four players.
func (g *Game) minBidPoints() int {
	if g.numSeats() == 4 {
		return 4
	}
	return 3
}
