package game

import "fmt"

// Suit represents the four card suits plus Joker
type Suit string

const (
	Spades   Suit = "spades"
	Hearts   Suit = "hearts"
	Diamonds Suit = "diamonds"
	Clubs    Suit = "clubs"
	NoSuit   Suit = "joker" // for Joker card
)

// Rank represents card ranks from 2 to Ace plus Joker
type Rank string

const (
	Two   Rank = "2"
	Three Rank = "3"
	Four  Rank = "4"
	Five  Rank = "5"
	Six   Rank = "6"
	Seven Rank = "7"
	Eight Rank = "8"
	Nine  Rank = "9"
	Ten   Rank = "10"
	Jack  Rank = "J"
	Queen Rank = "Q"
	King  Rank = "K"
	Ace   Rank = "A"
	Joker Rank = "JOKER"
)

// Card represents a playing card
type Card struct {
	Suit Suit `json:"suit"`
	Rank Rank `json:"rank"`
}

// Abbreviation returns the single-letter abbreviation for the suit.
func (s Suit) Abbreviation() string {
	switch s {
	case Spades:
		return "S"
	case Hearts:
		return "H"
	case Diamonds:
		return "D"
	case Clubs:
		return "C"
	case NoSuit:
		return "J"
	default:
		return ""
	}
}

// String returns a string representation of the card (e.g., "SA", "H10", "JOKER")
func (c Card) String() string {
	if c.Rank == Joker {
		return "JOKER"
	}
	return c.Suit.Abbreviation() + string(c.Rank)
}

// ParseCard converts a string like "SA", "H10", "JOKER" into a Card
func ParseCard(s string) (Card, error) {
	if s == "JOKER" {
		return Card{Suit: NoSuit, Rank: Joker}, nil
	}
	if len(s) < 2 {
		return Card{}, fmt.Errorf("invalid card string: %s", s)
	}

	var suit Suit
	switch s[0] {
	case 'S', 's':
		suit = Spades
	case 'H', 'h':
		suit = Hearts
	case 'D', 'd':
		suit = Diamonds
	case 'C', 'c':
		suit = Clubs
	default:
		return Card{}, fmt.Errorf("invalid suit: %c", s[0])
	}

	rankStr := s[1:]
	var rank Rank
	switch rankStr {
	case string(Two):
		rank = Two
	case string(Three):
		rank = Three
	case string(Four):
		rank = Four
	case string(Five):
		rank = Five
	case string(Six):
		rank = Six
	case string(Seven):
		rank = Seven
	case string(Eight):
		rank = Eight
	case string(Nine):
		rank = Nine
	case string(Ten):
		rank = Ten
	case string(Jack):
		rank = Jack
	case string(Queen):
		rank = Queen
	case string(King):
		rank = King
	case string(Ace):
		rank = Ace
	default:
		return Card{}, fmt.Errorf("invalid rank: %s", rankStr)
	}

	return Card{Suit: suit, Rank: rank}, nil
}

// PointValue returns the point value of the card (A, K, Q, J, 10 = 1 point)
func (c Card) PointValue() int {
	switch c.Rank {
	case Ace, King, Queen, Jack, Ten:
		return 1
	default:
		return 0
	}
}

// IsMighty checks if the card is the Mighty (SA unless spades are trump, then DA)
func (c Card) IsMighty(trump Suit) bool {
	if trump == Spades {
		return c.Suit == Diamonds && c.Rank == Ace
	}
	return c.Suit == Spades && c.Rank == Ace
}

// IsJoker checks if the card is the Joker
func (c Card) IsJoker() bool {
	return c.Rank == Joker
}

// IsRipper checks if the card is the Ripper (C3 unless clubs are trump, then S3)
func (c Card) IsRipper(trump Suit) bool {
	if trump == Clubs {
		return c.Suit == Spades && c.Rank == Three
	}
	return c.Suit == Clubs && c.Rank == Three
}

// IsMagicCard checks if the card is Mighty or Joker
func (c Card) IsMagicCard(trump Suit) bool {
	return c.IsMighty(trump) || c.IsJoker()
}

// RankValue returns numeric value for rank comparison (higher is better)
func (c Card) RankValue() int {
	switch c.Rank {
	case Two:
		return 2
	case Three:
		return 3
	case Four:
		return 4
	case Five:
		return 5
	case Six:
		return 6
	case Seven:
		return 7
	case Eight:
		return 8
	case Nine:
		return 9
	case Ten:
		return 10
	case Jack:
		return 11
	case Queen:
		return 12
	case King:
		return 13
	case Ace:
		return 14
	case Joker:
		return 15
	default:
		return 0
	}
}

// Equals checks if two cards are equal
func (c Card) Equals(other Card) bool {
	return c.Suit == other.Suit && c.Rank == other.Rank
}
