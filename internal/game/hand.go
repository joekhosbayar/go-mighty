package game

// Hand represents a single hand (deal) in the game
type Hand struct {
	HandNo     int
	DealerSeat int
	Phase      GamePhase

	// Bidding
	Bids          []Bid
	HighestBid    *Bid
	PassedSeats   map[int]bool // tracks who has passed
	CurrentBidder int          // seat of current bidder

	// Contract
	Contract        *Contract
	DeclarerSeat    int
	PartnerSeat     int // -1 until revealed
	PartnerRevealed bool

	// Cards
	Kitty       []Card
	Discarded   []Card
	PlayerHands [][]Card // indexed by seat

	// Tricks
	Tricks       []*Trick
	CurrentTrick *Trick
	PointsBySeat map[int]int // points taken by each seat

	// State
	RipperPlayed bool // Ripper has been played this hand
	JokerRipped  bool // Joker has been neutralized
}

// NewHand creates a new hand
func NewHand(handNo int, dealerSeat int, numPlayers int) *Hand {
	return &Hand{
		HandNo:          handNo,
		DealerSeat:      dealerSeat,
		Phase:           PhaseBidding,
		Bids:            make([]Bid, 0),
		PassedSeats:     make(map[int]bool),
		CurrentBidder:   dealerSeat,
		DeclarerSeat:    -1,
		PartnerSeat:     -1,
		PartnerRevealed: false,
		Kitty:           make([]Card, 0),
		Discarded:       make([]Card, 0),
		PlayerHands:     make([][]Card, numPlayers),
		Tricks:          make([]*Trick, 0, 10),
		PointsBySeat:    make(map[int]int),
		RipperPlayed:    false,
		JokerRipped:     false,
	}
}

// SetPlayerHands sets the dealt hands for all players
func (h *Hand) SetPlayerHands(hands [][]Card) {
	h.PlayerHands = hands
}

// SetKitty sets the kitty cards
func (h *Hand) SetKitty(kitty []Card) {
	h.Kitty = kitty
}

// AddBid adds a bid to the hand
func (h *Hand) AddBid(bid Bid) error {
	if h.Phase != PhaseBidding {
		return ErrInvalidPhase
	}

	if bid.SeatNo != h.CurrentBidder {
		return ErrNotPlayerTurn
	}

	if h.PassedSeats[bid.SeatNo] {
		return ErrPlayerAlreadyPassed
	}

	// Validate bid is higher than current highest
	if !bid.Passed && h.HighestBid != nil {
		if !bid.IsHigherThan(*h.HighestBid) {
			return ErrBidTooLow
		}
	}

	// Validate bid points are in range (13-20)
	if !bid.Passed && (bid.Points < 13 || bid.Points > 20) {
		return ErrInvalidBid
	}

	h.Bids = append(h.Bids, bid)

	if bid.Passed {
		h.PassedSeats[bid.SeatNo] = true
	} else {
		h.HighestBid = &bid
	}

	return nil
}

// NextBidder advances to the next bidder
func (h *Hand) NextBidder(numPlayers int) int {
	h.CurrentBidder = (h.CurrentBidder + 1) % numPlayers
	return h.CurrentBidder
}

// IsBiddingComplete checks if bidding is finished
func (h *Hand) IsBiddingComplete(numPlayers int) bool {
	// All passed = redeal
	if len(h.PassedSeats) == numPlayers {
		return true
	}

	// Only one player hasn't passed and there's a bid
	if h.HighestBid != nil && len(h.PassedSeats) == numPlayers-1 {
		return true
	}

	return false
}

// FinalizeBidding sets the contract and moves to kitty phase
func (h *Hand) FinalizeBidding() error {
	if h.HighestBid == nil {
		return ErrInvalidBid
	}

	h.DeclarerSeat = h.HighestBid.SeatNo
	h.Contract = &Contract{
		DeclarerSeat: h.DeclarerSeat,
		Points:       h.HighestBid.Points,
		Trump:        h.HighestBid.Trump,
		NoFriend:     false,
	}

	h.Phase = PhaseKitty
	return nil
}

// PickupKitty adds kitty cards to declarer's hand
func (h *Hand) PickupKitty() error {
	if h.Phase != PhaseKitty {
		return ErrInvalidPhase
	}

	if h.DeclarerSeat < 0 || h.DeclarerSeat >= len(h.PlayerHands) {
		return ErrInvalidSeat
	}

	// Add kitty to declarer's hand
	h.PlayerHands[h.DeclarerSeat] = append(h.PlayerHands[h.DeclarerSeat], h.Kitty...)
	h.Phase = PhaseDiscard

	return nil
}

// Discard removes cards from declarer's hand and adds them to discard pile
func (h *Hand) Discard(cards []Card) error {
	if h.Phase != PhaseDiscard {
		return ErrInvalidPhase
	}

	if len(cards) != 3 {
		return ErrInvalidMove
	}

	// Verify cards are in declarer's hand and remove them
	declarerHand := h.PlayerHands[h.DeclarerSeat]
	for _, card := range cards {
		found := false
		for i, handCard := range declarerHand {
			if handCard.Equals(card) {
				// Remove card
				declarerHand = append(declarerHand[:i], declarerHand[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return ErrCardNotInHand
		}
	}

	h.PlayerHands[h.DeclarerSeat] = declarerHand
	h.Discarded = cards
	h.Phase = PhaseCallingPartner

	return nil
}

// CallPartner sets how the declarer is calling their partner
func (h *Hand) CallPartner(call PartnerCall) error {
	if h.Phase != PhaseCallingPartner {
		return ErrInvalidPhase
	}

	if h.Contract == nil {
		return ErrInvalidMove
	}

	h.Contract.PartnerCall = &call

	if call.Type == PartnerCallNoFriend {
		h.Contract.NoFriend = true
		h.PartnerSeat = -1
	}

	h.Phase = PhasePlaying

	return nil
}

// StartTrick begins a new trick
func (h *Hand) StartTrick(leaderSeat int) {
	trickNo := len(h.Tricks) + 1
	h.CurrentTrick = NewTrick(trickNo, leaderSeat)
}

// PlayCard plays a card to the current trick
func (h *Hand) PlayCard(seatNo int, card Card, numPlayers int) error {
	if h.Phase != PhasePlaying {
		return ErrInvalidPhase
	}

	if h.CurrentTrick == nil {
		return ErrInvalidMove
	}

	// Verify it's player's turn
	expectedSeat := (h.CurrentTrick.LeaderSeat + len(h.CurrentTrick.Cards)) % numPlayers
	if seatNo != expectedSeat {
		return ErrNotPlayerTurn
	}

	// Verify card is in hand
	playerHand := h.PlayerHands[seatNo]
	cardIndex := -1
	for i, handCard := range playerHand {
		if handCard.Equals(card) {
			cardIndex = i
			break
		}
	}
	if cardIndex == -1 {
		return ErrCardNotInHand
	}

	// Remove card from hand
	h.PlayerHands[seatNo] = append(playerHand[:cardIndex], playerHand[cardIndex+1:]...)

	// Add to trick
	h.CurrentTrick.AddCard(seatNo, card)

	// Check for Ripper and Joker interaction
	if h.Contract != nil && card.IsRipper(h.Contract.Trump.Suit) {
		h.RipperPlayed = true
	}

	return nil
}

// CompleteTrick finishes the current trick and determines winner
func (h *Hand) CompleteTrick(numPlayers int) (int, error) {
	if h.CurrentTrick == nil || !h.CurrentTrick.IsComplete(numPlayers) {
		return -1, ErrInvalidMove
	}

	winnerSeat := h.DetermineWinner(h.CurrentTrick)
	h.CurrentTrick.WinnerSeat = winnerSeat

	// Add points to winner
	h.PointsBySeat[winnerSeat] += h.CurrentTrick.Points

	// Check if partner is revealed by this trick
	if h.Contract != nil && h.Contract.PartnerCall != nil && !h.PartnerRevealed {
		if h.Contract.PartnerCall.Type == PartnerCallCard && h.Contract.PartnerCall.Card != nil {
			// Check if the called card was played
			for _, play := range h.CurrentTrick.Cards {
				if play.Card.Equals(*h.Contract.PartnerCall.Card) {
					h.PartnerSeat = play.SeatNo
					h.PartnerRevealed = true
					break
				}
			}
		} else if h.Contract.PartnerCall.Type == PartnerCallFirstTrick && len(h.Tricks) == 0 {
			// Winner of first trick is partner
			h.PartnerSeat = winnerSeat
			h.PartnerRevealed = true
		}
	}

	// Store completed trick
	h.Tricks = append(h.Tricks, h.CurrentTrick)
	h.CurrentTrick = nil

	return winnerSeat, nil
}

// DetermineWinner determines the winner of a trick based on Mighty rules
func (h *Hand) DetermineWinner(trick *Trick) int {
	if len(trick.Cards) == 0 {
		return -1
	}

	trump := Suit("")
	if h.Contract != nil && !h.Contract.Trump.NoTrump {
		trump = h.Contract.Trump.Suit
	}

	winningPlay := trick.Cards[0]

	for _, play := range trick.Cards[1:] {
		if h.BeatsCard(play.Card, winningPlay.Card, trick, trump) {
			winningPlay = play
		}
	}

	return winningPlay.SeatNo
}

// BeatsCard determines if card1 beats card2 in the context of a trick
func (h *Hand) BeatsCard(card1, card2 Card, trick *Trick, trump Suit) bool {
	// Mighty beats everything
	if card1.IsMighty(trump) {
		return true
	}
	if card2.IsMighty(trump) {
		return false
	}

	// Joker beats everything except Mighty (unless ripped)
	if card1.IsJoker() {
		return !h.JokerRipped || !h.RipperPlayed
	}
	if card2.IsJoker() {
		return false
	}

	leadSuit := trick.LeadSuit()
	if leadSuit == nil {
		return false
	}

	// Both trump
	if card1.Suit == trump && card2.Suit == trump {
		return card1.RankValue() > card2.RankValue()
	}

	// card1 is trump, card2 is not
	if card1.Suit == trump {
		return true
	}

	// card2 is trump, card1 is not
	if card2.Suit == trump {
		return false
	}

	// Neither is trump - must follow lead suit
	if card1.Suit == *leadSuit && card2.Suit == *leadSuit {
		return card1.RankValue() > card2.RankValue()
	}

	// card1 follows suit, card2 doesn't
	if card1.Suit == *leadSuit {
		return true
	}

	// card2 follows suit, card1 doesn't
	if card2.Suit == *leadSuit {
		return false
	}

	// Neither follows suit - first card wins
	return false
}

// IsComplete checks if the hand is complete (all 10 tricks played)
func (h *Hand) IsComplete() bool {
	return len(h.Tricks) == 10
}

// CalculateHandPoints calculates the total points taken by declarer's team
func (h *Hand) CalculateHandPoints() int {
	declarerPoints := h.PointsBySeat[h.DeclarerSeat]

	// Add points from discarded cards
	for _, card := range h.Discarded {
		declarerPoints += card.PointValue()
	}

	// Add partner's points if not playing alone
	if !h.Contract.NoFriend && h.PartnerSeat >= 0 {
		declarerPoints += h.PointsBySeat[h.PartnerSeat]
	}

	return declarerPoints
}

// CalculateWeakHandValue calculates hand value for redeal eligibility
// Mighty (SA) = 0, Joker = -1, A/K/Q/J = +1, 10 = +0.5, others = 0
func CalculateWeakHandValue(hand []Card, trump Suit) float64 {
	value := 0.0

	for _, card := range hand {
		if card.IsMighty(trump) {
			value += 0
		} else if card.IsJoker() {
			value -= 1
		} else {
			switch card.Rank {
			case Ace, King, Queen, Jack:
				value += 1
			case Ten:
				value += 0.5
			default:
				value += 0
			}
		}
	}

	return value
}

// CanRedealForWeakHand checks if a hand qualifies for redeal (≤ 0.5 points)
func (h *Hand) CanRedealForWeakHand(seatNo int) bool {
	if seatNo < 0 || seatNo >= len(h.PlayerHands) {
		return false
	}

	trump := Spades // default for weak hand calculation
	value := CalculateWeakHandValue(h.PlayerHands[seatNo], trump)

	return value <= 0.5
}
