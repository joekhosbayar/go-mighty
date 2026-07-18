// Package game implements the rules and logic for validating and applying moves in the Mighty card game.
package game

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidMove is returned when a move is invalid.
var ErrInvalidMove = errors.New("invalid move")

var suitRank = map[Suit]int{
	Clubs:    1,
	Diamonds: 2,
	Hearts:   3,
	Spades:   4,
}

var validRanks = map[Rank]bool{
	Ace: true, King: true, Queen: true, Jack: true, Ten: true, Nine: true,
	Eight: true, Seven: true, Six: true, Five: true, Four: true, Three: true, Two: true,
}

// Power constants for the Mighty engine, used to determine card strength in a trick.
const (
	// PowerMighty represents the strength of the Mighty card (highest).
	PowerMighty = 1000
	// PowerJoker represents the base strength of the Joker card.
	PowerJoker = 500
	// PowerTrump represents the base strength of a trump suit card.
	PowerTrump = 100
	// PowerLead represents the base strength of a card matching the lead suit.
	PowerLead = 10
	// PowerBase represents the base strength of any other card.
	PowerBase = 0
)

// ValidateMove checks if a move is valid for the current game state.
func (g *Game) ValidateMove(playerID string, moveType MoveType, payload any) error {
	// 1. Check if player is in the game
	p := g.GetPlayer(playerID)
	if p == nil {
		return fmt.Errorf("%w: player not in game", ErrInvalidMove)
	}

	// 2. Check turn
	if g.Status == PhasePlaying && g.Players[g.CurrentTurn].ID != playerID {
		return fmt.Errorf("%w: not your turn", ErrInvalidMove)
	}
	// For other phases, checking turn depends on the phase logic (e.g. bidding is rotational)

	switch moveType {
	case MoveBid:
		return g.validateBid(p, payload)
	case MovePass:
		return g.validatePass(p)
	case MoveDiscard:
		return g.validateDiscard(p, payload)
	case MoveCallPartner:
		return g.validateCallPartner(p, payload)
	case MovePlayCard:
		return g.validatePlayCard(p, payload)
	default:
		return errors.New("unknown move type")
	}
}

// GetPlayer retrieves a player by their ID.
func (g *Game) GetPlayer(id string) *Player {
	for _, p := range g.Players {
		if p != nil && p.ID == id {
			return p
		}
	}

	return nil
}

// Helper to check if a card is in hand.
func (p *Player) HasCard(c Card) bool {
	for _, handCard := range p.Hand {
		if handCard.Suit == c.Suit && handCard.Rank == c.Rank {
			return true
		}
	}

	return false
}

// validateBid checks if the bid is valid
// Payload expected: Bid struct.
func (g *Game) validateBid(p *Player, payload any) error {
	if g.Status != PhaseBidding {
		return fmt.Errorf("%w: not in bidding phase", ErrInvalidMove)
	}

	bid, ok := payload.(Bid)
	if !ok {
		return errors.New("invalid payload for bid")
	}

	// Must be this player's turn to bid?
	// Bidding is rotational usually.
	// But in Mighty, multiple people can bid?
	// Rules: "The dealer opens bidding... thereafter previous declarer opens".
	// "The last remaining bidder becomes declarer".
	// Usually strict rotation or "speak when you want"?
	// Standard rules imply rotation. We will enforce rotation based on CurrentTurn.
	if g.Players[g.CurrentTurn].ID != p.ID {
		return fmt.Errorf("%w: not your turn to bid", ErrInvalidMove)
	}

	if bid.Points < 3 || bid.Points > 10 {
		return fmt.Errorf("%w: bid points must be between 3 and 10", ErrInvalidMove)
	}

	if bid.IsNoTrump {
		if bid.Suit != None {
			return fmt.Errorf("%w: no-trump bids must use suit 'none'", ErrInvalidMove)
		}
	} else {
		if _, ok := suitRank[bid.Suit]; !ok {
			return fmt.Errorf("%w: invalid bid suit", ErrInvalidMove)
		}
	}

	// Must be higher than current bid
	if g.CurrentBid != nil {
		if bid.Points <= g.CurrentBid.Points {
			return fmt.Errorf("%w: bid must be strictly higher than current bid", ErrInvalidMove)
		}
	}

	return nil
}

func (g *Game) validatePass(p *Player) error {
	if g.Status != PhaseBidding {
		return fmt.Errorf("%w: not in bidding phase", ErrInvalidMove)
	}

	if g.Players[g.CurrentTurn].ID != p.ID {
		return fmt.Errorf("%w: not your turn to pass", ErrInvalidMove)
	}

	return nil
}

// validateDiscard
// Payload: []Card (3 cards).
func (g *Game) validateDiscard(p *Player, payload any) error {
	if g.Status != PhaseExchanging {
		return fmt.Errorf("%w: not in exchanging phase", ErrInvalidMove)
	}

	if g.Players[g.Declarer].ID != p.ID {
		return fmt.Errorf("%w: only declarer can discard", ErrInvalidMove)
	}

	cards, ok := payload.([]Card)
	if !ok {
		return errors.New("invalid payload for discard")
	}

	if len(cards) != 3 {
		return fmt.Errorf("%w: must discard exactly 3 cards", ErrInvalidMove)
	}

	// Verify player actually has these cards
	// Note: At this point, player has 13 cards (Hand + Kitty)
	for _, c := range cards {
		if !p.HasCard(c) {
			return fmt.Errorf("%w: do not hold card %s", ErrInvalidMove, c)
		}
	}

	return nil
}

// asCallPartnerMove normalizes the two accepted payload shapes.
func asCallPartnerMove(payload any) (CallPartnerMove, error) {
	switch v := payload.(type) {
	case CallPartnerMove:
		return v, nil
	case Card:
		return CallPartnerMove{Card: &v}, nil
	default:
		return CallPartnerMove{}, errors.New("invalid payload for partner call")
	}
}

// validateCallPartner
// Payload: CallPartnerMove (or legacy Card).
func (g *Game) validateCallPartner(p *Player, payload any) error {
	if g.Status != PhaseCalling {
		return fmt.Errorf("%w: not in calling phase", ErrInvalidMove)
	}

	if g.Players[g.Declarer].ID != p.ID {
		return fmt.Errorf("%w: only declarer call partner", ErrInvalidMove)
	}

	move, err := asCallPartnerMove(payload)
	if err != nil {
		return err
	}

	if move.Card != nil && move.NoFriend {
		return fmt.Errorf("%w: choose a card or no_friend, not both", ErrInvalidMove)
	}

	if move.Card == nil && !move.NoFriend {
		return fmt.Errorf("%w: call_partner requires a card or no_friend", ErrInvalidMove)
	}

	if move.Card != nil {
		isJoker := move.Card.Suit == None && move.Card.Rank == Joker
		if !isJoker {
			if _, ok := suitRank[move.Card.Suit]; !ok || !validRanks[move.Card.Rank] {
				return fmt.Errorf("%w: invalid partner card", ErrInvalidMove)
			}
		}
	}

	return nil
}

// validatePlayCard
// Payload: PlayCardMove.
func (g *Game) validatePlayCard(p *Player, payload any) error {
	if g.Status != PhasePlaying {
		return fmt.Errorf("%w: not in playing phase", ErrInvalidMove)
	}

	if g.Players[g.CurrentTurn].ID != p.ID {
		return fmt.Errorf("%w: not your turn", ErrInvalidMove)
	}

	move, ok := payload.(PlayCardMove)
	if !ok {
		// Fallback for simple Card payload if necessary, but we prefer PlayCardMove
		card, ok := payload.(Card)
		if !ok {
			return errors.New("invalid payload for play card")
		}

		move = PlayCardMove{Card: card}
	}

	card := move.Card
	if !p.HasCard(card) {
		return fmt.Errorf("%w: do not hold card %s", ErrInvalidMove, card)
	}

	// Trick Validation Logic
	currentTrickIdx := len(g.Tricks) - 1
	if currentTrickIdx < 0 {
		return errors.New("no active trick")
	}

	t := g.Tricks[currentTrickIdx]

	// 0. Late-Game Special Card Forcing (Trick 8 and 9)
	cardsLeft := len(p.Hand)
	hasMighty := p.HasMighty(g)
	hasJoker := p.HasRank(Joker)
	isPlayingMightyOrJoker := g.IsMighty(card) || card.Rank == Joker

	if cardsLeft == 3 && hasMighty && hasJoker && !isPlayingMightyOrJoker {
		return fmt.Errorf("%w: must play mighty or joker on 3rd to last trick", ErrInvalidMove)
	}
	if cardsLeft == 2 && (hasMighty || hasJoker) && !isPlayingMightyOrJoker {
		return fmt.Errorf("%w: must play mighty or joker on 2nd to last trick", ErrInvalidMove)
	}

	// 1. Forced Play (Joker Called)
	if t.JokerCalled && p.HasRank(Joker) {
		// "The only exception is that if the joker holder also has the mighty in which case she may choose to play the mighty"
		if card.Rank != Joker && !g.IsMighty(card) {
			return fmt.Errorf("%w: joker called, must play joker or mighty", ErrInvalidMove)
		}
	}

	// 2. Leading Rules
	if len(t.Cards) == 0 {
		// First trick lead rules
		if len(g.Tricks) == 1 {
			if card.Rank == Joker {
				return fmt.Errorf("%w: cannot lead joker on first trick", ErrInvalidMove)
			}
			if g.IsMighty(card) {
				return fmt.Errorf("%w: cannot lead mighty on first trick", ErrInvalidMove)
			}
			if card.Suit == g.Trump && p.HasNonTrumpMightyJoker(g) {
				return fmt.Errorf("%w: cannot lead trump on first trick unless holding only trump/special cards", ErrInvalidMove)
			}
		}

		// Joker lead must declare the suit followers owe; called_suit is
		// meaningless on any other play.
		if card.Rank == Joker {
			if _, ok := suitRank[move.CalledSuit]; !ok {
				return fmt.Errorf("%w: joker lead requires called_suit", ErrInvalidMove)
			}
		} else if move.CalledSuit != "" {
			return fmt.Errorf("%w: called_suit only valid when leading the joker", ErrInvalidMove)
		}

		// Joker Caller option
		if move.CallJoker && !g.IsJokerCaller(card) {
			return fmt.Errorf("%w: only joker caller can call joker", ErrInvalidMove)
		}
		// Joker Caller loses power/ability on first and last trick
		if move.CallJoker && (len(g.Tricks) == 1 || len(g.Tricks) == 10) {
			return fmt.Errorf("%w: cannot call joker on first or last trick", ErrInvalidMove)
		}

		return nil
	}

	if move.CalledSuit != "" {
		return fmt.Errorf("%w: called_suit only valid when leading the joker", ErrInvalidMove)
	}

	// 3. Following Suit
	lead := t.LeadSuit
	// Special Rule: First Hand Restrictions
	if len(g.Tricks) == 1 {
		if card.Rank == Joker {
			return fmt.Errorf("%w: cannot play joker on first trick", ErrInvalidMove)
		}
		if g.IsMighty(card) {
			// Mighty can only be played if the led suit matches its base suit AND it is the only card of that suit
			if lead != card.Suit || p.GetSuitCount(lead) > 1 {
				return fmt.Errorf("%w: cannot play mighty on first trick unless it is your only card of the led suit", ErrInvalidMove)
			}
		}
	}

	if card.Suit != lead {
		// Allowed if playing Mighty or Joker
		if g.IsMighty(card) || card.Rank == Joker {
			return nil
		}

		// Otherwise, must follow suit if possible
		if p.HasSuit(lead) {
			return fmt.Errorf("%w: must follow suit %s", ErrInvalidMove, lead)
		}
	}

	return nil
}

// Helpers

// IsMighty checks if a card is the Mighty card given the current trump suit.
func (g *Game) IsMighty(c Card) bool {
	// Usually Ace of Spades.
	// If Spades is Trump, Ace of Diamonds is Mighty.
	if g.Trump == Spades {
		return c.Suit == Diamonds && c.Rank == Ace
	}

	return c.Suit == Spades && c.Rank == Ace
}

// IsJokerCaller checks if a card is the Joker Caller card given the current trump suit.
func (g *Game) IsJokerCaller(c Card) bool {
	// Usually Three of Clubs.
	// If Clubs is Trump, Three of Spades is Joker Caller.
	if g.Trump == Clubs {
		return c.Suit == Spades && c.Rank == Three
	}

	return c.Suit == Clubs && c.Rank == Three
}

// friendSeat returns the seat of the mystery friend (the holder of the called
// partner card), or -1 when there is no friend or the card is unheld (e.g. the
// declarer discarded it into the kitty before calling). It scans current hands
// and every played trick card, so it is correct at any point after the friend
// is called and needs no stored field — it survives Redis reloads for free.
func (g *Game) friendSeat() int {
	if g.IsNoFriend || g.PartnerCard == nil {
		return -1
	}

	pc := *g.PartnerCard

	for _, p := range g.Players {
		if p == nil {
			continue
		}

		for _, c := range p.Hand {
			if c.Suit == pc.Suit && c.Rank == pc.Rank {
				return p.Seat
			}
		}
	}

	for _, t := range g.Tricks {
		for _, played := range t.Cards {
			if played.Card.Suit == pc.Suit && played.Card.Rank == pc.Rank {
				return played.Seat
			}
		}
	}

	return -1
}

// trickRevealsFriend reports whether winning this trick outs the friend: it
// holds a scoring card to defend, or the friend won it with the joker. (The
// mighty is an Ace, so it already counts as a scoring card.)
func trickRevealsFriend(t Trick, friendSeat int) bool {
	for _, played := range t.Cards {
		if played.Card.IsPointCard() {
			return true
		}

		if played.Seat == friendSeat && played.Card.Rank == Joker {
			return true
		}
	}

	return false
}

// HasRank checks if a player has a card of the specified rank in their hand.
func (p *Player) HasRank(r Rank) bool {
	for _, c := range p.Hand {
		if c.Rank == r {
			return true
		}
	}

	return false
}

// HasSuit checks if a player has a card of the specified suit in their hand.
func (p *Player) HasSuit(s Suit) bool {
	for _, c := range p.Hand {
		if c.Suit == s {
			return true
		}
	}

	return false
}

// HasNonTrump checks if a player has any non-trump cards in their hand.
func (p *Player) HasNonTrump(trump Suit) bool {
	for _, c := range p.Hand {
		if c.Suit != trump && c.Rank != Joker {
			// Joker is not really a suit, but effectively non-trump usually unless declared?
			// Actually Joker is neither.
			// Logic: If I have a Heart (non-trump), I can't lead Trump.
			return true
		}
	}

	return false
}

// HasMighty checks if a player holds the Mighty card.
func (p *Player) HasMighty(g *Game) bool {
	for _, c := range p.Hand {
		if g.IsMighty(c) {
			return true
		}
	}
	return false
}

// HasNonTrumpMightyJoker checks if a player has any cards that are NOT Trump, Mighty, or Joker.
func (p *Player) HasNonTrumpMightyJoker(g *Game) bool {
	for _, c := range p.Hand {
		if c.Suit != g.Trump && !g.IsMighty(c) && c.Rank != Joker {
			return true
		}
	}
	return false
}

// GetSuitCount returns the number of cards the player holds of the given suit.
func (p *Player) GetSuitCount(s Suit) int {
	count := 0
	for _, c := range p.Hand {
		if c.Suit == s {
			count++
		}
	}
	return count
}

// ApplyMove applies the move to the game state
// Assumes ValidateMove has already been called.
func (g *Game) ApplyMove(playerID string, moveType MoveType, payload any) error {
	p := g.GetPlayer(playerID)

	switch moveType {
	case MoveBid:
		bid, ok := payload.(Bid)
		if !ok {
			return errors.New("invalid payload for bid")
		}

		bid.PlayerID = playerID // Ensure playerID is set

		g.CurrentBid = &bid
		g.Declarer = p.Seat                  // Potential declarer

		if bid.Points == 10 || len(g.PassedPlayers) == 4 {
			// Auto-resolve if maximum bid is reached or all others passed
			g.Status = PhaseExchanging
			g.Contract = g.CurrentBid
			g.Trump = g.Contract.Suit
			g.CurrentTurn = g.Declarer

			declarer := g.Players[g.Declarer]
			declarer.Hand = append(declarer.Hand, g.Kitty...)
			g.Kitty = nil
		} else {
			g.advanceToNextBidder()
		}

	case MovePass:
		g.PassedPlayers[p.Seat] = true
		g.advanceToNextBidder()
		// Check if bidding ended
		if len(g.PassedPlayers) == 4 && g.CurrentBid != nil {
			g.Status = PhaseExchanging
			// Set final declarer (should be already set by last bid)
			g.Contract = g.CurrentBid
			g.CurrentTurn = g.Declarer
			g.Trump = g.Contract.Suit

			// Give kitty to declarer
			declarer := g.Players[g.Declarer]
			declarer.Hand = append(declarer.Hand, g.Kitty...)
			g.Kitty = nil // Empty kitty
		} else if len(g.PassedPlayers) == 5 {
			// Everyone passed: throw the hand in and redeal.
			g.Bids = nil
			g.CurrentBid = nil
			g.Contract = nil
			g.Declarer = -1
			g.PassedPlayers = make(map[int]bool)
			g.PartnerCard = nil
			g.PartnerSeat = -1
			g.IsNoFriend = false
			g.Trump = ""
			g.Tricks = make([]Trick, 0)
			g.Start()
		}

	case MoveDiscard:
		cards, ok := payload.([]Card)
		if !ok {
			return errors.New("invalid payload for discard")
		}

		// Remove cards from hand
		newHand := []Card{}
		discardPoints := []Card{}

		for _, c := range p.Hand {
			isDiscarded := false

			for _, dc := range cards {
				if c.Suit == dc.Suit && c.Rank == dc.Rank {
					isDiscarded = true
					break
				}
			}

			if !isDiscarded {
				newHand = append(newHand, c)
			} else if c.IsPointCard() {
				discardPoints = append(discardPoints, c)
			}
		}

		p.Hand = newHand
		// Declarer gets points from discard?
		// "May score points from discarded scoring cards"
		p.Points = append(p.Points, discardPoints...)

		g.Status = PhaseCalling

	case MoveCallPartner:
		move, err := asCallPartnerMove(payload)
		if err != nil {
			return err
		}

		if move.NoFriend {
			g.IsNoFriend = true
			g.PartnerCard = nil
		} else {
			g.PartnerCard = move.Card
		}

		g.Status = PhasePlaying
		// Start playing
		g.CurrentTurn = g.Declarer // Declarer leads first trick
		g.Tricks = append(g.Tricks, Trick{Cards: []PlayedCard{}})

	case MovePlayCard:
		move, ok := payload.(PlayCardMove)
		if !ok {
			// Fallback for Card payload
			card, ok := payload.(Card)
			if !ok {
				return errors.New("invalid payload for play card")
			}
			move = PlayCardMove{Card: card}
		}

		card := move.Card

		// Remove from hand
		newHand := []Card{}

		for _, c := range p.Hand {
			if c.Suit == card.Suit && c.Rank == card.Rank {
				continue
			}

			newHand = append(newHand, c)
		}

		p.Hand = newHand

		// Add to trick
		idx := len(g.Tricks) - 1
		g.Tricks[idx].Cards = append(g.Tricks[idx].Cards, PlayedCard{
			PlayerID: playerID,
			Seat:     p.Seat,
			Card:     card,
		})

		// Set Lead Suit if first card
		if len(g.Tricks[idx].Cards) == 1 {
			g.Tricks[idx].LeadSuit = card.Suit
			if card.Rank == Joker {
				g.Tricks[idx].LeadSuit = move.CalledSuit
			}

			// Handle Joker Caller
			if move.CallJoker && g.IsJokerCaller(card) {
				g.Tricks[idx].JokerCalled = true
			}
		}

		// Turn moves to next
		g.CurrentTurn = (g.CurrentTurn + 1) % 5

		// Check if trick finished
		if len(g.Tricks[idx].Cards) == 5 {
			winnerSeat, points := g.ResolveTrick(g.Tricks[idx])
			g.Tricks[idx].Winner = winnerSeat

			// Reveal the friend once they defend: they win a trick that holds a
			// scoring card, or take it with the joker. A pointless win stays
			// ambiguous, so it does not reveal.
			if g.PartnerSeat < 0 {
				if fs := g.friendSeat(); fs >= 0 && winnerSeat == fs && trickRevealsFriend(g.Tricks[idx], fs) {
					g.PartnerSeat = fs
				}
			}

			// Give points to winner
			winner := g.Players[winnerSeat]
			winner.Points = append(winner.Points, points...)

			// Winner leads next
			g.CurrentTurn = winnerSeat

			if len(g.Tricks) == 10 {
				g.Status = PhaseFinished
				declarerScore, partnerScore := g.CalculateFinalScore()

				g.Scores = make(map[string]int, len(g.Players))
				for _, player := range g.Players {
					if player != nil {
						g.Scores[player.ID] = 0
					}
				}
				// This score model stores the declarer/friend team result for the round.
				// Opponents are explicitly kept at 0 in this per-round map.
				if g.Declarer >= 0 && g.Declarer < len(g.Players) && g.Players[g.Declarer] != nil {
					g.Scores[g.Players[g.Declarer].ID] = int(declarerScore)
				}

				if fs := g.friendSeat(); fs >= 0 && fs < len(g.Players) && g.Players[fs] != nil {
					g.Scores[g.Players[fs].ID] = int(partnerScore)
				}
			} else {
				g.Tricks = append(g.Tricks, Trick{Cards: []PlayedCard{}})
			}
		}
	}

	g.Version++
	g.UpdatedAt = time.Now()

	return nil
}

// ResolveTrick determines the winner and points.
func (g *Game) ResolveTrick(t Trick) (int, []Card) {
	winnerIdx := 0
	maxPower := -1
	points := []Card{}

	// Calculate trick number (1-10)
	trickNum := len(g.Tricks)

	for i, pc := range t.Cards {
		if pc.Card.IsPointCard() {
			points = append(points, pc.Card)
		}

		power := g.CalculatePower(pc.Card, t, trickNum)
		if power > maxPower {
			maxPower = power
			winnerIdx = i
		}
	}

	return t.Cards[winnerIdx].Seat, points
}

// CalculatePower determines the contextual strength of a card.
func (g *Game) CalculatePower(c Card, t Trick, trickNum int) int {
	// 1. Mighty beats everything
	if g.IsMighty(c) {
		return PowerMighty
	}

	// 2. Joker Logic
	if c.Rank == Joker {
		// Joker loses power if:
		// - Played in first or last trick
		if trickNum == 1 || trickNum == 10 {
			return PowerBase
		}
		// - Joker Caller led and called Joker
		if t.JokerCalled {
			return PowerBase
		}
		// - Mighty is in the trick
		for _, pc := range t.Cards {
			if g.IsMighty(pc.Card) {
				return PowerBase
			}
		}

		return PowerJoker
	}

	// 3. Trump suit
	if c.Suit == g.Trump {
		return PowerTrump + RankValue(c.Rank)
	}

	// 4. Lead suit
	if c.Suit == t.LeadSuit {
		return PowerLead + RankValue(c.Rank)
	}

	// 5. Standard rank
	return PowerBase + RankValue(c.Rank)
}

// Beats returns true if c1 beats c2.
func (g *Game) Beats(c1, c2 Card, t Trick) bool {
	trickNum := len(g.Tricks)
	return g.CalculatePower(c1, t, trickNum) > g.CalculatePower(c2, t, trickNum)
}

// CalculateFinalScore calculates the final points for the declarer and friend.
func (g *Game) CalculateFinalScore() (float64, float64) {
	if g.Contract == nil {
		return 0, 0
	}

	fs := g.friendSeat()

	// Count tricks won by the caller team (declarer + friend).
	tricksWon := 0

	for _, t := range g.Tricks {
		if t.Winner == g.Declarer || t.Winner == fs {
			tricksWon++
		}
	}

	// User's example says "7-spade bid... win 7 out of 10 tricks".
	contractGoal := g.Contract.Points

	score := 0.0
	diff := tricksWon - contractGoal

	if diff >= 0 {
		// Won!
		score = float64(contractGoal*10 + diff*5)
	} else {
		// Lost!
		// "loses on a 7-hearts bid by capturing only 6 tricks. That would be -7*10 = -70"
		// "Had they only captured 5 tricks, that would be -7*10 – 1*5 = -75"
		score = float64(-contractGoal * 10)
		if diff < -1 {
			score += float64((diff + 1) * 5)
		}
	}

	// Multipliers
	if g.Contract.IsNoTrump {
		score *= 2
	}

	if g.IsNoFriend {
		score *= 2
	}

	if contractGoal == 10 {
		score *= 2
	}

	// Cap at 800/-800 as per user rule
	if score > 800 {
		score = 800
	}

	if score < -800 {
		score = -800
	}

	friendScore := score / 2.0
	if g.IsNoFriend || fs < 0 {
		friendScore = 0 // No friend to share with.
	}

	return score, friendScore
}

// RankValue returns the numerical value of a rank for comparison.
func RankValue(r Rank) int {
	switch r {
	case Ace:
		return 14
	case King:
		return 13
	case Queen:
		return 12
	case Jack:
		return 11
	case Ten:
		return 10
	case Nine:
		return 9
	case Eight:
		return 8
	case Seven:
		return 7
	case Six:
		return 6
	case Five:
		return 5
	case Four:
		return 4
	case Three:
		return 3
	case Two:
		return 2
	case Joker:
		return 0 // Joker value handled separately by power logic
	default:
		return 0
	}
}

// advanceToNextBidder advances the current turn to the next player who has not passed.
func (g *Game) advanceToNextBidder() {
	if len(g.PassedPlayers) >= 5 {
		return
	}
	for {
		g.CurrentTurn = (g.CurrentTurn + 1) % 5
		if !g.PassedPlayers[g.CurrentTurn] {
			break
		}
	}
}
