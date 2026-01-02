package game

import "errors"

// Game errors
var (
	ErrInsufficientCards    = errors.New("insufficient cards in deck")
	ErrInvalidBid           = errors.New("invalid bid")
	ErrBidTooLow            = errors.New("bid too low")
	ErrPlayerAlreadyPassed  = errors.New("player has already passed")
	ErrInvalidTrump         = errors.New("invalid trump suit")
	ErrInvalidMove          = errors.New("invalid move")
	ErrNotPlayerTurn        = errors.New("not player's turn")
	ErrCardNotInHand        = errors.New("card not in hand")
	ErrMustFollowSuit       = errors.New("must follow suit")
	ErrInvalidPhase         = errors.New("invalid game phase")
	ErrGameNotStarted       = errors.New("game not started")
	ErrGameAlreadyStarted   = errors.New("game already started")
	ErrInvalidPlayerCount   = errors.New("invalid player count")
	ErrInvalidSeat          = errors.New("invalid seat number")
	ErrSeatOccupied         = errors.New("seat already occupied")
	ErrPlayerNotFound       = errors.New("player not found")
	ErrNotDeclarer          = errors.New("only declarer can perform this action")
	ErrKittyAlreadyPicked   = errors.New("kitty already picked")
	ErrPartnerAlreadyCalled = errors.New("partner already called")
	ErrInvalidPartnerCall   = errors.New("invalid partner call")
	ErrCannotLeadTrump      = errors.New("cannot lead trump on first trick unless only trumps in hand")
)

// Trump represents the trump suit or no-trump
type Trump struct {
	Suit    Suit // empty string for no-trump
	NoTrump bool
}

// GamePhase represents the current phase of the game
type GamePhase string

const (
	PhaseWaiting        GamePhase = "waiting"         // waiting for players
	PhaseBidding        GamePhase = "bidding"         // bidding phase
	PhaseKitty          GamePhase = "kitty"           // declarer picking kitty
	PhaseDiscard        GamePhase = "discard"         // declarer discarding
	PhaseCallingPartner GamePhase = "calling_partner" // declarer calling partner
	PhasePlaying        GamePhase = "playing"         // trick-taking phase
	PhaseHandComplete   GamePhase = "hand_complete"   // hand finished, scoring
	PhaseGameComplete   GamePhase = "game_complete"   // game finished
)

// PlayerRole represents a player's role in the current hand
type PlayerRole string

const (
	RoleUndecided PlayerRole = "undecided" // before partner revealed
	RoleDeclarer  PlayerRole = "declarer"
	RolePartner   PlayerRole = "partner"
	RoleOpponent  PlayerRole = "opponent"
)

// Bid represents a player's bid
type Bid struct {
	SeatNo int   // who made the bid
	Points int   // 13-20
	Trump  Trump // trump suit or no-trump
	Passed bool  // true if player passed
}

// NewBid creates a new bid
func NewBid(seatNo int, points int, trump Trump) Bid {
	return Bid{
		SeatNo: seatNo,
		Points: points,
		Trump:  trump,
		Passed: false,
	}
}

// NewPass creates a pass bid
func NewPass(seatNo int) Bid {
	return Bid{
		SeatNo: seatNo,
		Passed: true,
	}
}

// IsHigherThan checks if this bid is higher than another
// Higher points beat lower points
// No-trump beats same points with a suit
func (b Bid) IsHigherThan(other Bid) bool {
	if b.Passed {
		return false
	}
	if other.Passed {
		return true
	}
	if b.Points > other.Points {
		return true
	}
	if b.Points == other.Points && b.Trump.NoTrump && !other.Trump.NoTrump {
		return true
	}
	return false
}

// Contract represents the final contract after bidding
type Contract struct {
	DeclarerSeat int
	Points       int // bid points (13-20)
	Trump        Trump
	NoFriend     bool // declarer playing alone
	PartnerCall  *PartnerCall
}

// PartnerCallType represents how the partner is chosen
type PartnerCallType string

const (
	PartnerCallCard       PartnerCallType = "card"        // call a specific card
	PartnerCallFirstTrick PartnerCallType = "first_trick" // winner of first trick
	PartnerCallNoFriend   PartnerCallType = "no_friend"   // play alone
)

// PartnerCall represents how the declarer calls their partner
type PartnerCall struct {
	Type     PartnerCallType
	Card     *Card // for card call
	LeadSuit *Suit // optional: for 20 no-trump, request specific lead
}

// Trick represents a single trick in play
type Trick struct {
	TrickNo    int
	LeaderSeat int
	Cards      []CardPlay // cards played in order
	WinnerSeat int        // -1 if not complete
	Points     int        // point value of cards in trick
}

// CardPlay represents a card played by a player
type CardPlay struct {
	SeatNo int
	Card   Card
}

// NewTrick creates a new trick
func NewTrick(trickNo int, leaderSeat int) *Trick {
	return &Trick{
		TrickNo:    trickNo,
		LeaderSeat: leaderSeat,
		Cards:      make([]CardPlay, 0, 5),
		WinnerSeat: -1,
		Points:     0,
	}
}

// AddCard adds a card to the trick
func (t *Trick) AddCard(seatNo int, card Card) {
	t.Cards = append(t.Cards, CardPlay{SeatNo: seatNo, Card: card})
	t.Points += card.PointValue()
}

// IsComplete checks if all players have played
func (t *Trick) IsComplete(numPlayers int) bool {
	return len(t.Cards) == numPlayers
}

// LeadSuit returns the suit of the first card played (nil for empty trick)
func (t *Trick) LeadSuit() *Suit {
	if len(t.Cards) == 0 {
		return nil
	}
	suit := t.Cards[0].Card.Suit
	return &suit
}

// RedealReason represents why a redeal was requested
type RedealReason string

const (
	RedealAllPassed RedealReason = "all_passed" // all five players passed
	RedealWeakHand  RedealReason = "weak_hand"  // hand value ≤ 0.5 points
	RedealManual    RedealReason = "manual"     // manual redeal
)
