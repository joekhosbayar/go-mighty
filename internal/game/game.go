package game

import "time"

// Phase represents the current state of the game
type Phase string

const (
	PhaseWaiting    Phase = "waiting"
	PhaseBidding    Phase = "bidding"
	PhaseExchanging Phase = "exchanging" // Declarer exchanges cards
	PhaseCalling    Phase = "calling"    // Declarer calls partner
	PhasePlaying    Phase = "playing"
	PhaseFinished   Phase = "finished"
)

// MoveType represents the type of action a player performs
type MoveType string

const (
	MoveBid         MoveType = "bid"
	MovePass        MoveType = "pass"
	MoveDiscard     MoveType = "discard"
	MoveCallPartner MoveType = "call_partner"
	MovePlayCard    MoveType = "play_card"
)

// GameConfig holds configuration for the game
type GameConfig struct {
	MaxPlayers   int `json:"max_players"`
	WinningScore int `json:"winning_score"` // usually 13-20
}

// Player represents a participant in the game
type Player struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Seat        int    `json:"seat"`             // 0-4
	Hand        []Card `json:"hand,omitempty"`   // hidden from others in JSON
	Points      []Card `json:"points,omitempty"` // point cards taken
	IsConnected bool   `json:"is_connected"`
}

// Bid represents a player's bid
type Bid struct {
	PlayerID  string `json:"player_id"`
	Points    int    `json:"points"`
	Suit      Suit   `json:"suit"` // Suit or None for NoTrump
	IsNoTrump bool   `json:"is_no_trump"`
}

// GameState represents the complete state of a single game
type GameState struct {
	ID      string     `json:"id"`
	Status  Phase      `json:"status"`
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
	Trump        Suit    `json:"trump"`
	Tricks       []Trick `json:"tricks"`
	CurrentTrick *Trick  `json:"current_trick"`

	// Scoring
	Scores map[string]int `json:"scores"` // Points taken by each player (accumulated)

	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Trick represents a single round of 5 cards
type Trick struct {
	Cards    []PlayedCard `json:"cards"`
	LeadSuit Suit         `json:"lead_suit"`
	Winner   int          `json:"winner"` // Seat index
}

type PlayedCard struct {
	PlayerID string `json:"player_id"`
	Seat     int    `json:"seat"`
	Card     Card   `json:"card"`
}

// NewGame creates a new game instance
func NewGame(id string) *GameState {
	g := &GameState{
		ID:            id,
		Status:        PhaseWaiting,
		Players:       [5]*Player{},
		PassedPlayers: make(map[int]bool),
		Tricks:        make([]Trick, 0),
		Scores:        make(map[string]int),
		Declarer:      -1,
		PartnerSeat:   -1,
		Version:       1,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	return g
}

// Start deals the cards and starts the bidding phase
func (g *GameState) Start() {
	deck := NewDeck()
	deck.Shuffle()
	hands, kitty := deck.Deal()

	for i, h := range hands {
		if g.Players[i] != nil {
			g.Players[i].Hand = h
			g.Players[i].Points = []Card{}
		}
	}
	g.Kitty = kitty
	g.Status = PhaseBidding

	// Determine first bidder
	// Usually dealer's left (or previous declarer).
	// We'll simplistic: Seat 0 starts.
	// Or Random dealer?
	g.CurrentTurn = 0 // Seat 0 bids first
}
