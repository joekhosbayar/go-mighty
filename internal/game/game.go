package game

import (
	"time"
)

// PlayerState represents a player's state in the game
type PlayerState struct {
	PlayerID  string
	SeatNo    int
	Connected bool
	LastPing  time.Time
	Role      PlayerRole
}

// Game represents a game session with multiple hands
type Game struct {
	GameID      string
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	Status      GamePhase
	Variant     string
	MaxPlayers  int

	// Players
	Players []*PlayerState

	// Current hand
	CurrentHand *Hand
	HandNo      int

	// Game history
	Hands []*Hand

	// Settings
	Options GameOptions
}

// GameOptions contains game variant settings
type GameOptions struct {
	MinBid           int  // default 13
	AllowNoTrump     bool // default true
	AllowNoFriend    bool // default true
	AllowRaiseBid    bool // declarer can raise bid after seeing kitty
	AllowChangeTrump bool // declarer can change trump after seeing kitty
}

// DefaultGameOptions returns standard Mighty game options
func DefaultGameOptions() GameOptions {
	return GameOptions{
		MinBid:           13,
		AllowNoTrump:     true,
		AllowNoFriend:    true,
		AllowRaiseBid:    true,
		AllowChangeTrump: true,
	}
}

// NewGame creates a new game instance
func NewGame(gameID string, maxPlayers int) *Game {
	return &Game{
		GameID:     gameID,
		CreatedAt:  time.Now(),
		Status:     PhaseWaiting,
		Variant:    "mighty-5p-standard",
		MaxPlayers: maxPlayers,
		Players:    make([]*PlayerState, maxPlayers),
		Hands:      make([]*Hand, 0),
		HandNo:     0,
		Options:    DefaultGameOptions(),
	}
}

// AddPlayer adds a player to a specific seat
func (g *Game) AddPlayer(playerID string, seatNo int) error {
	if g.Status != PhaseWaiting {
		return ErrGameAlreadyStarted
	}

	if seatNo < 0 || seatNo >= g.MaxPlayers {
		return ErrInvalidSeat
	}

	if g.Players[seatNo] != nil {
		return ErrSeatOccupied
	}

	g.Players[seatNo] = &PlayerState{
		PlayerID:  playerID,
		SeatNo:    seatNo,
		Connected: true,
		LastPing:  time.Now(),
		Role:      RoleUndecided,
	}

	return nil
}

// RemovePlayer removes a player from the game
func (g *Game) RemovePlayer(seatNo int) error {
	if seatNo < 0 || seatNo >= g.MaxPlayers {
		return ErrInvalidSeat
	}

	g.Players[seatNo] = nil
	return nil
}

// GetPlayer returns the player at a specific seat
func (g *Game) GetPlayer(seatNo int) *PlayerState {
	if seatNo < 0 || seatNo >= g.MaxPlayers {
		return nil
	}
	return g.Players[seatNo]
}

// GetPlayerBySeatNo finds a player by seat number
func (g *Game) GetPlayerBySeatNo(seatNo int) (*PlayerState, error) {
	if seatNo < 0 || seatNo >= g.MaxPlayers {
		return nil, ErrInvalidSeat
	}

	player := g.Players[seatNo]
	if player == nil {
		return nil, ErrPlayerNotFound
	}

	return player, nil
}

// GetPlayerByID finds a player by their ID
func (g *Game) GetPlayerByID(playerID string) (*PlayerState, error) {
	for _, player := range g.Players {
		if player != nil && player.PlayerID == playerID {
			return player, nil
		}
	}
	return nil, ErrPlayerNotFound
}

// IsReadyToStart checks if the game can start (all seats filled)
func (g *Game) IsReadyToStart() bool {
	if g.Status != PhaseWaiting {
		return false
	}

	for _, player := range g.Players {
		if player == nil {
			return false
		}
	}

	return true
}

// Start begins the game
func (g *Game) Start() error {
	if !g.IsReadyToStart() {
		return ErrInvalidPlayerCount
	}

	now := time.Now()
	g.StartedAt = &now
	g.Status = PhaseBidding

	return nil
}

// StartNewHand begins a new hand
func (g *Game) StartNewHand(dealerSeat int) error {
	if g.Status == PhaseWaiting {
		return ErrGameNotStarted
	}

	g.HandNo++
	g.CurrentHand = NewHand(g.HandNo, dealerSeat, g.MaxPlayers)

	// Deal cards
	deck := NewDeck()
	deck.Shuffle()
	playerHands, kitty, err := deck.Deal(g.MaxPlayers)
	if err != nil {
		return err
	}

	g.CurrentHand.SetPlayerHands(playerHands)
	g.CurrentHand.SetKitty(kitty)

	return nil
}

// CompleteCurrentHand finalizes the current hand and calculates scores
func (g *Game) CompleteCurrentHand() error {
	if g.CurrentHand == nil {
		return ErrInvalidMove
	}

	if !g.CurrentHand.IsComplete() {
		return ErrInvalidMove
	}

	g.CurrentHand.Phase = PhaseHandComplete
	g.Hands = append(g.Hands, g.CurrentHand)

	return nil
}

// GetNextDealer determines who deals the next hand
// Rule: declarer's partner deals next, or declarer if played alone
func (g *Game) GetNextDealer() int {
	if g.CurrentHand == nil {
		return 0 // first hand, random or seat 0
	}

	if g.CurrentHand.Contract != nil && g.CurrentHand.Contract.NoFriend {
		// Declarer played alone, they deal next
		return g.CurrentHand.DeclarerSeat
	}

	if g.CurrentHand.PartnerSeat >= 0 {
		// Partner deals next
		return g.CurrentHand.PartnerSeat
	}

	// Fallback: next seat
	return (g.CurrentHand.DealerSeat + 1) % g.MaxPlayers
}

// ValidateCardPlay validates if a card can be legally played
func (g *Game) ValidateCardPlay(seatNo int, card Card) error {
	if g.CurrentHand == nil {
		return ErrInvalidPhase
	}

	if g.CurrentHand.Phase != PhasePlaying {
		return ErrInvalidPhase
	}

	hand := g.CurrentHand.PlayerHands[seatNo]

	// Check if card is in hand
	hasCard := false
	for _, c := range hand {
		if c.Equals(card) {
			hasCard = true
			break
		}
	}
	if !hasCard {
		return ErrCardNotInHand
	}

	// If this is the first card of the trick
	if g.CurrentHand.CurrentTrick == nil || len(g.CurrentHand.CurrentTrick.Cards) == 0 {
		// First trick: cannot lead trump unless only have trumps
		if len(g.CurrentHand.Tricks) == 0 {
			if g.CurrentHand.Contract != nil && !g.CurrentHand.Contract.Trump.NoTrump {
				trump := g.CurrentHand.Contract.Trump.Suit
				if card.Suit == trump && !card.IsMighty(trump) && !card.IsJoker() {
					// Check if player has only trumps
					hasNonTrump := false
					for _, c := range hand {
						if c.Suit != trump && !c.IsMighty(trump) && !c.IsJoker() {
							hasNonTrump = true
							break
						}
					}
					if hasNonTrump {
						return ErrCannotLeadTrump
					}
				}
			}
		}
		return nil
	}

	// Must follow suit if possible
	leadSuit := g.CurrentHand.CurrentTrick.LeadSuit()
	if leadSuit != nil && card.Suit != *leadSuit && !card.IsJoker() {
		// Check if player has any cards of lead suit
		hasLeadSuit := false
		for _, c := range hand {
			if c.Suit == *leadSuit {
				hasLeadSuit = true
				break
			}
		}
		if hasLeadSuit {
			return ErrMustFollowSuit
		}
	}

	return nil
}

// UpdatePlayerRole updates player roles based on current hand state
func (g *Game) UpdatePlayerRole(seatNo int) PlayerRole {
	if g.CurrentHand == nil {
		return RoleUndecided
	}

	if seatNo == g.CurrentHand.DeclarerSeat {
		return RoleDeclarer
	}

	if g.CurrentHand.PartnerRevealed && seatNo == g.CurrentHand.PartnerSeat {
		return RolePartner
	}

	if g.CurrentHand.Contract != nil && g.CurrentHand.Contract.NoFriend {
		return RoleOpponent
	}

	return RoleUndecided
}

// CalculateScore calculates the score for a completed hand
// Returns map of seatNo -> score
func (g *Game) CalculateScore() (map[int]int, error) {
	if g.CurrentHand == nil || !g.CurrentHand.IsComplete() {
		return nil, ErrInvalidPhase
	}

	contract := g.CurrentHand.Contract
	if contract == nil {
		return nil, ErrInvalidMove
	}

	B := contract.Points                     // bid
	P := g.CurrentHand.CalculateHandPoints() // points taken
	M := g.Options.MinBid                    // minimum bid (13)

	success := P >= B

	var S int
	if success {
		// S = 2 × (B − M) + (P − B)
		S = 2*(B-M) + (P - B)
	} else {
		// S = B − P
		S = B - P
	}

	// Apply multipliers
	multiplier := 1

	// Run: declarer team took all 20 points
	if success && P == 20 {
		multiplier *= 2
	}

	// Back run: defenders took ≥ 11 points
	if !success && (20-P) >= 11 {
		multiplier *= 2
	}

	// No-trump
	if contract.Trump.NoTrump {
		multiplier *= 2
	}

	// No friend
	if contract.NoFriend {
		multiplier *= 2
	}

	S *= multiplier

	// Build score map
	scores := make(map[int]int)

	if success {
		// Declarer gets +2S
		scores[g.CurrentHand.DeclarerSeat] = 2 * S

		// Partner gets +S (if exists)
		if !contract.NoFriend && g.CurrentHand.PartnerSeat >= 0 {
			scores[g.CurrentHand.PartnerSeat] = S
		}

		// Opponents get -S each
		for seatNo := 0; seatNo < g.MaxPlayers; seatNo++ {
			if seatNo != g.CurrentHand.DeclarerSeat && seatNo != g.CurrentHand.PartnerSeat {
				scores[seatNo] = -S
			}
		}
	} else {
		// Declarer gets -2S
		scores[g.CurrentHand.DeclarerSeat] = -2 * S

		// Partner gets -S (if exists)
		if !contract.NoFriend && g.CurrentHand.PartnerSeat >= 0 {
			scores[g.CurrentHand.PartnerSeat] = -S
		}

		// Opponents get +S each
		for seatNo := 0; seatNo < g.MaxPlayers; seatNo++ {
			if seatNo != g.CurrentHand.DeclarerSeat && seatNo != g.CurrentHand.PartnerSeat {
				scores[seatNo] = S
			}
		}
	}

	return scores, nil
}

// GetMighty returns the Mighty card for the current trump
func (g *Game) GetMighty() Card {
	if g.CurrentHand == nil || g.CurrentHand.Contract == nil {
		// Default Mighty
		return Card{Suit: Spades, Rank: Ace}
	}

	trump := g.CurrentHand.Contract.Trump.Suit
	if trump == Spades {
		return Card{Suit: Diamonds, Rank: Ace}
	}
	return Card{Suit: Spades, Rank: Ace}
}

// GetRipper returns the Ripper card for the current trump
func (g *Game) GetRipper() Card {
	if g.CurrentHand == nil || g.CurrentHand.Contract == nil {
		// Default Ripper
		return Card{Suit: Clubs, Rank: Three}
	}

	trump := g.CurrentHand.Contract.Trump.Suit
	if trump == Clubs {
		return Card{Suit: Spades, Rank: Three}
	}
	return Card{Suit: Clubs, Rank: Three}
}

// GetJoker returns the Joker card
func (g *Game) GetJoker() Card {
	return Card{Suit: NoSuit, Rank: Joker}
}

// IsGameComplete checks if the game session is complete
// (Could be based on number of hands, score limit, etc.)
func (g *Game) IsGameComplete() bool {
	// For now, games don't auto-complete
	return g.Status == PhaseGameComplete
}

// Complete marks the game as complete
func (g *Game) Complete() {
	now := time.Now()
	g.CompletedAt = &now
	g.Status = PhaseGameComplete
}
