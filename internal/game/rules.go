package game

import (
	"fmt"
	"time"
)

// ErrInvalidMove is returned when a move is invalid
var ErrInvalidMove = fmt.Errorf("invalid move")

// ValidateMove checks if a move is valid for the current game state
func (g *GameState) ValidateMove(playerID string, moveType MoveType, payload interface{}) error {
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
		return fmt.Errorf("unknown move type")
	}
}

func (g *GameState) GetPlayer(id string) *Player {
	for _, p := range g.Players {
		if p != nil && p.ID == id {
			return p
		}
	}
	return nil
}

// Helper to check if a card is in hand
func (p *Player) HasCard(c Card) bool {
	for _, handCard := range p.Hand {
		if handCard.Suit == c.Suit && handCard.Rank == c.Rank {
			return true
		}
	}
	return false
}

// validateBid checks if the bid is valid
// Payload expected: Bid struct
func (g *GameState) validateBid(p *Player, payload interface{}) error {
	if g.Status != PhaseBidding {
		return fmt.Errorf("%w: not in bidding phase", ErrInvalidMove)
	}

	bid, ok := payload.(Bid)
	if !ok {
		return fmt.Errorf("invalid payload for bid")
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

	if bid.Points < 13 || bid.Points > 20 {
		return fmt.Errorf("%w: bid points must be between 13 and 20", ErrInvalidMove)
	}

	// Must be higher than current bid
	if g.CurrentBid != nil {
		if bid.Points < g.CurrentBid.Points {
			return fmt.Errorf("%w: bid must be higher", ErrInvalidMove)
		}
		if bid.Points == g.CurrentBid.Points {
			// NoTrump beats Suit
			if !bid.IsNoTrump || g.CurrentBid.IsNoTrump {
				return fmt.Errorf("%w: insufficient bid to raise", ErrInvalidMove)
			}
		}
	}

	return nil
}

func (g *GameState) validatePass(p *Player) error {
	if g.Status != PhaseBidding {
		return fmt.Errorf("%w: not in bidding phase", ErrInvalidMove)
	}
	if g.Players[g.CurrentTurn].ID != p.ID {
		return fmt.Errorf("%w: not your turn to pass", ErrInvalidMove)
	}
	return nil
}

// validateDiscard
// Payload: []Card (3 cards)
func (g *GameState) validateDiscard(p *Player, payload interface{}) error {
	if g.Status != PhaseExchanging {
		return fmt.Errorf("%w: not in exchanging phase", ErrInvalidMove)
	}
	if g.Players[g.Declarer].ID != p.ID {
		return fmt.Errorf("%w: only declarer can discard", ErrInvalidMove)
	}

	cards, ok := payload.([]Card)
	if !ok {
		return fmt.Errorf("invalid payload for discard")
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

// validateCallPartner
// Payload: Card (the partner card)
func (g *GameState) validateCallPartner(p *Player, payload interface{}) error {
	if g.Status != PhaseCalling {
		return fmt.Errorf("%w: not in calling phase", ErrInvalidMove)
	}
	if g.Players[g.Declarer].ID != p.ID {
		return fmt.Errorf("%w: only declarer call partner", ErrInvalidMove)
	}

	// Check payload
	// It's just a card.
	_, ok := payload.(Card)
	if !ok {
		return fmt.Errorf("invalid payload for partner call")
	}

	return nil
}

// validatePlayCard
// Payload: Card
func (g *GameState) validatePlayCard(p *Player, payload interface{}) error {
	if g.Status != PhasePlaying {
		return fmt.Errorf("%w: not in playing phase", ErrInvalidMove)
	}
	if g.Players[g.CurrentTurn].ID != p.ID {
		return fmt.Errorf("%w: not your turn", ErrInvalidMove)
	}

	card, ok := payload.(Card)
	if !ok {
		return fmt.Errorf("invalid payload")
	}

	if !p.HasCard(card) {
		return fmt.Errorf("%w: do not hold card %s", ErrInvalidMove, card)
	}

	// Trick Validation Logic
	// Get the current trick (last trick in the list)
	currentTrickIdx := len(g.Tricks) - 1
	if currentTrickIdx >= 0 && len(g.Tricks[currentTrickIdx].Cards) > 0 {
		lead := g.Tricks[currentTrickIdx].LeadSuit
		// If holding lead suit, MUST follow suit
		// Exceptions: Mighty, Joker

		isMighty := g.IsMighty(card)
		isJoker := card.Rank == Joker

		// If player plays Mighty, allowed.
		if isMighty {
			return nil
		}

		// If player plays Joker
		// "Joker can be played anytime"
		// BUT "Joker loses power if Ripper led/played?" - This affects power, not legality usually.
		// "Ripper may force Joker to be played"
		// If Ripper was led, and player has Joker, MUST play Joker?
		// Rules check: "Ripper... Can force the Joker to be played"
		// This implies if Ripper led, holder of Joker MUST play it if they can?
		// "If the Ripper is led... the Joker must be played." -> YES.

		// Implementation of Ripper force:
		ripperLed := false
		if len(g.Tricks[currentTrickIdx].Cards) > 0 {
			leadCard := g.Tricks[currentTrickIdx].Cards[0].Card
			if g.IsRipper(leadCard) {
				ripperLed = true
			}
		}

		if ripperLed && p.HasRank(Joker) {
			if card.Rank != Joker {
				// Must play Joker!
				// UNLESS they have Mighty? Mighty > all.
				// Rules don't explicitly say Mighty saves Joker from Ripper.
				// Usually Joker MUST be played.
				return fmt.Errorf("%w: ripper led, must play joker", ErrInvalidMove)
			}
		}

		// Regular follow suit
		// If NOT Mighty and NOT Joker (and not forced), check suit
		if !isJoker && card.Suit != lead {
			// Player is reneging (playing off-suit).
			// Allowed ONLY if player has NO cards of lead suit.
			if p.HasSuit(lead) {
				// WAIT! If they have lead suit...
				// Can they play Joker? Yes ("Joker can be played anytime").
				// Can they play Mighty? Yes.
				// But if they play random off-suit card, that is invalid.
				return fmt.Errorf("%w: must follow suit %s", ErrInvalidMove, lead)
			}
		}
	} else {
		// Leading (First card of trick)
		// "No trump lead on trick one unless holding only trumps" (Optional rule? Rules.md says yes)
		// Usually if it's the very first trick of the hand.
		if len(g.Tricks) == 0 {
			if card.Suit == g.Trump {
				// Check if player has ONLY trumps (plus Mighty/Joker?)
				// Simplified: if they have any non-trump, cannot lead trump.
				if p.HasNonTrump(g.Trump) {
					// Exception: Mighty can be led anytime? Mighty is part of trump suit effectively?
					// Usually Mighty is considered a separate entity or part of Spades/Diamonds.
					// If Mighty is led, it counts as its suit (Spades/Diamonds).
					return fmt.Errorf("%w: cannot lead trump on first trick", ErrInvalidMove)
				}
			}
		}
	}

	return nil
}

// Helpers

func (g *GameState) IsMighty(c Card) bool {
	// Spades Ace is Mighty usually.
	// If Spades is Trump, Diamond Ace is Mighty.
	if g.Trump == Spades {
		return c.Suit == Diamonds && c.Rank == Ace
	}
	return c.Suit == Spades && c.Rank == Ace
}

func (g *GameState) IsRipper(c Card) bool {
	// Clubs 3 is Ripper.
	// If Clubs is Trump, Spades 3 is Ripper.
	if g.Trump == Clubs {
		return c.Suit == Spades && c.Rank == Three
	}
	return c.Suit == Clubs && c.Rank == Three
}

func (p *Player) HasRank(r Rank) bool {
	for _, c := range p.Hand {
		if c.Rank == r {
			return true
		}
	}
	return false
}

func (p *Player) HasSuit(s Suit) bool {
	for _, c := range p.Hand {
		if c.Suit == s {
			return true
		}
	}
	return false
}

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

// ApplyMove applies the move to the game state
// Assumes ValidateMove has already been called
func (g *GameState) ApplyMove(playerID string, moveType MoveType, payload interface{}) error {
	p := g.GetPlayer(playerID)

	switch moveType {
	case MoveBid:
		bid := payload.(Bid)
		bid.PlayerID = playerID // Ensure playerID is set

		// If pass
		if bid.Points == 0 {
			p := g.GetPlayer(playerID)
			if p != nil {
				g.PassedPlayers[p.Seat] = true
			}
		} else {
			g.CurrentBid = &bid
			g.Declarer = p.Seat // Potential declarer
		}
		// In rotation, move turn to next player?
		// Or if everyone passes?
		// Simplified: We assume bidding continues until 4 passes?
		// For now simple implementation: Just set bid and move turn.
		g.CurrentTurn = (g.CurrentTurn + 1) % 5

	case MovePass:
		g.PassedPlayers[p.Seat] = true
		g.CurrentTurn = (g.CurrentTurn + 1) % 5
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
			// Redeal?
			// TODO: Implement redeal logic or just error/finish
			g.Status = PhaseFinished
		}

	case MoveDiscard:
		cards := payload.([]Card)
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
			} else {
				if c.IsPointCard() {
					discardPoints = append(discardPoints, c)
				}
			}
		}
		p.Hand = newHand
		// Declarer gets points from discard?
		// "May score points from discarded scoring cards"
		p.Points = append(p.Points, discardPoints...)

		g.Status = PhaseCalling

	case MoveCallPartner:
		card := payload.(Card)
		g.PartnerCard = &card
		g.Status = PhasePlaying
		// Start playing
		g.CurrentTurn = g.Declarer // Declarer leads first trick
		g.Tricks = append(g.Tricks, Trick{Cards: []PlayedCard{}})

	case MovePlayCard:
		card := payload.(Card)
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
			// Should handle Mighty/Joker lead suit rules?
			// Usually Mighty/Joker don't set suit if led? Or they do?
			// Simplified: First card sets suit unless it's Joker?
			g.Tricks[idx].LeadSuit = card.Suit
		}

		// Turn moves to next
		g.CurrentTurn = (g.CurrentTurn + 1) % 5

		// Check if trick finished
		if len(g.Tricks[idx].Cards) == 5 {
			winnerSeat, points := g.ResolveTrick(g.Tricks[idx])
			g.Tricks[idx].Winner = winnerSeat

			// Give points to winner
			winner := g.Players[winnerSeat]
			winner.Points = append(winner.Points, points...)

			// Winner leads next
			g.CurrentTurn = winnerSeat

			if len(g.Tricks) == 10 {
				g.Status = PhaseFinished
				// TODO: Calculate final scores
			} else {
				g.Tricks = append(g.Tricks, Trick{Cards: []PlayedCard{}})
			}
		}
	}

	g.Version++
	g.UpdatedAt = time.Now()

	return nil
}

// ResolveTrick determines the winner and points
func (g *GameState) ResolveTrick(t Trick) (int, []Card) {
	winnerIdx := 0
	winningCard := t.Cards[0].Card
	points := []Card{}

	for i, pc := range t.Cards {
		if pc.Card.IsPointCard() {
			points = append(points, pc.Card)
		}

		if i == 0 {
			continue
		}

		// Compare pc.Card with winningCard
		if g.Beats(pc.Card, winningCard, t.LeadSuit) {
			winningCard = pc.Card
			winnerIdx = i
		}
	}

	return t.Cards[winnerIdx].Seat, points
}

// Beats returns true if c1 beats c2
func (g *GameState) Beats(c1, c2 Card, leadSuit Suit) bool {
	// 1. Mighty beats everything
	if g.IsMighty(c1) {
		return true
	}
	if g.IsMighty(c2) {
		return false
	}

	// 2. Joker beats everything else (unless lost power?)
	// TODO: Check if joker lost power in this trick (if Ripper was played)
	// Simplified: Joker beats non-Mighty
	if c1.Rank == Joker {
		return true
	}
	if c2.Rank == Joker {
		return false
	}

	// 3. Trump beats non-trump
	isTrump1 := c1.Suit == g.Trump
	isTrump2 := c2.Suit == g.Trump

	if isTrump1 && !isTrump2 {
		return true
	}
	if !isTrump1 && isTrump2 {
		return false
	}

	// 4. Same suit: Higher rank wins
	if c1.Suit == c2.Suit {
		return RankValue(c1.Rank) > RankValue(c2.Rank)
	}

	// 5. If c1 is lead suit and c2 is not (and not trump), c1 wins
	if c1.Suit == leadSuit && c2.Suit != leadSuit {
		return true
	}

	// c2 stays winner
	return false
}

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
	default:
		return 0
	}
}
