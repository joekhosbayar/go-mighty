// Package game provides game state definitions and core mechanics for the Mighty card game.
package game

import "time"

// Phase represents the current state of the game.
type Phase string

const (
	// PhaseWaiting indicates the game is waiting for players to join.
	PhaseWaiting Phase = "waiting"
	// PhaseBidding indicates players are currently bidding for the contract.
	PhaseBidding Phase = "bidding"
	// PhaseExchanging indicates the declarer is exchanging cards with the kitty.
	PhaseExchanging Phase = "exchanging" // Declarer exchanges cards
	// PhaseCalling indicates the declarer is calling a partner.
	PhaseCalling Phase = "calling" // Declarer calls partner
	// PhasePlaying indicates the trick-taking phase is in progress.
	PhasePlaying Phase = "playing"
	// PhaseFinished indicates the game has concluded.
	PhaseFinished Phase = "finished"
)

// MoveType represents the type of action a player performs.
type MoveType string

const (
	// MoveBid represents a bidding action.
	MoveBid MoveType = "bid"
	// MovePass represents a passing action during bidding.
	MovePass MoveType = "pass"
	// MoveDiscard represents a card discard action by the declarer.
	MoveDiscard MoveType = "discard"
	// MoveCallPartner represents a partner calling action by the declarer.
	MoveCallPartner MoveType = "call_partner"
	// MovePlayCard represents a card play action during the playing phase.
	MovePlayCard MoveType = "play_card"
	// MovePlayAgain represents voting to play another round.
	MovePlayAgain MoveType = "play_again"
	// MoveChangeConfig represents changing the game config (e.g. NumPlayers).
	MoveChangeConfig MoveType = "change_config"
)

// ChangeConfigMove represents the payload for changing game config.
type ChangeConfigMove struct {
	NumPlayers int `json:"num_players"`
}

// PlayCardMove represents the payload for playing a card.
type PlayCardMove struct {
	Card       Card `json:"card"`
	CallJoker  bool `json:"call_joker"`
	CalledSuit Suit `json:"called_suit,omitempty"` // required when leading the Joker
}

// CallPartnerMove represents the declarer's friend call: either a card
// (whose holder becomes the secret partner) or no_friend to play alone.
type CallPartnerMove struct {
	Card     *Card `json:"card,omitempty"`
	NoFriend bool  `json:"no_friend,omitempty"`
}

// Config holds configuration for the game.
type Config struct {
	MaxPlayers   int `json:"max_players"`
	WinningScore int `json:"winning_score"` // usually 3-10
}

// Player represents a participant in the game.
type Player struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Seat        int    `json:"seat"`             // 0-4
	Hand        []Card `json:"hand,omitempty"`   // hidden from others in JSON
	Points      []Card `json:"points,omitempty"` // point cards taken
	IsConnected bool   `json:"is_connected"`
}

// Bid represents a player's bid.
type Bid struct {
	PlayerID  string `json:"player_id"`
	Points    int    `json:"points"`
	Suit      Suit   `json:"suit"` // Suit or None for NoTrump
	IsNoTrump bool   `json:"is_no_trump"`
}

// Game represents the complete state of a single game.
type Game struct {
	ID      string     `json:"id"`
	Status  Phase      `json:"status"`
	Config  GameConfig `json:"config"`
	Players [5]*Player `json:"players"`
	Kitty   []Card     `json:"kitty,omitempty"` // hidden usually

	// Hand State
	Deck        Deck `json:"-"`
	CurrentTurn int  `json:"current_turn"` // Seat index 0-4
	Dealer      int  `json:"dealer"`       // Seat index

	// Bidding
	Bids          []Bid        `json:"bids"`
	CurrentBid    *Bid         `json:"current_bid"`
	Declarer      int          `json:"declarer"` // Seat index, -1 if none
	PassedPlayers map[int]bool `json:"passed_players"`

	// Contract
	Contract *Bid `json:"contract"`

	// Partner
	PartnerCard *Card `json:"partner_card"`
	PartnerSeat int   `json:"partner_seat"` // -1 if unknown or alone
	IsNoFriend  bool  `json:"is_no_friend"`

	// Play
	Trump  Suit    `json:"trump"`
	Tricks []Trick `json:"tricks"`

	// Scoring
	Scores         map[string]int `json:"scores"` // Final round scores: declarer full, revealed partner half, others 0. Card points live in Player.Points.
	TotalScores    map[string]int `json:"total_scores"` // Cumulative scores
	PlayAgainVotes map[int]bool   `json:"play_again_votes"` // Seats that voted to play again

	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Trick represents a single round of 5 cards.
type Trick struct {
	Cards       []PlayedCard `json:"cards"`
	LeadSuit    Suit         `json:"lead_suit"`
	Winner      int          `json:"winner"`       // Seat index
	JokerCalled bool         `json:"joker_called"` // If Joker Caller led and called Joker
}

// PlayedCard represents a card that has been played by a player during a trick.
type PlayedCard struct {
	PlayerID string `json:"player_id"`
	Seat     int    `json:"seat"`
	Card     Card   `json:"card"`
}

// New creates a new five-player game instance.
func New(id string) *Game {
	return NewWithConfig(id, DefaultConfig())
}

// NewWithConfig creates a new game with the given configuration.
func NewWithConfig(id string, cfg GameConfig) *Game {
	if cfg.NumPlayers == 0 {
		cfg.NumPlayers = 5
	}
	if cfg.FailDist == "" {
		cfg.FailDist = FailEqualSplit
	}
	g := &Game{
		ID:            id,
		Status:        PhaseWaiting,
		Config:        cfg,
		Players:       [5]*Player{},
		PassedPlayers: make(map[int]bool),
		Tricks:        make([]Trick, 0),
		Scores:        make(map[string]int),
		TotalScores:   make(map[string]int),
		PlayAgainVotes: make(map[int]bool),
		Declarer:      -1,
		PartnerSeat:   -1,
		Version:       1,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	return g
}

// IsFull checks if the game has all its seats filled.
func (g *Game) IsFull() bool {
	count := 0
	for i := 0; i < g.numSeats(); i++ {
		if g.Players[i] != nil {
			count++
		}
	}
	return count == g.numSeats()
}

// Start deals the cards and starts the bidding phase.
func (g *Game) Start() {
	deck := NewDeckFor(g.numSeats())
	deck.Shuffle()
	hands, kitty := deck.Deal(g.numSeats())

	for i, h := range hands {
		if g.Players[i] != nil {
			g.Players[i].Hand = h
			g.Players[i].Points = []Card{}
		}
	}

	g.Kitty = kitty
	g.Status = PhaseBidding

	g.CurrentTurn = 0 // Seat 0 bids first
}
