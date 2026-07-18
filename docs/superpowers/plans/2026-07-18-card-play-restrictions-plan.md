# Card Play Restrictions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement missing Mighty and Joker play restrictions for first and last tricks, and fix Mighty card default.

**Architecture:** Add specific conditional checks to `validatePlayCard` in `rules.go` to enforce trick-number-based restrictions. Update `IsMighty` for Spades trump. Create a new test file `rules_restrictions_test.go` to cleanly house all new constraint tests.

**Tech Stack:** Go 1.22+

## Global Constraints
- No external dependencies.
- Strict adherence to the spec's edge cases.
- All tests must pass, pristine test output.

---

### Task 1: Mighty Default Correction

**Files:**
- Modify: `internal/game/rules.go:341-348`
- Create: `internal/game/rules_restrictions_test.go`

**Interfaces:**
- Produces: Corrected `IsMighty` behavior where Trump Spades -> Diamonds Ace.

- [ ] **Step 1: Write the failing test**

Create `internal/game/rules_restrictions_test.go`:
```go
package game

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestIsMighty(t *testing.T) {
	g := New("test")
	g.Trump = Spades
	
	assert.True(t, g.IsMighty(Card{Suit: Diamonds, Rank: Ace}), "Spades trump -> Mighty is Ace of Diamonds")
	assert.False(t, g.IsMighty(Card{Suit: Clubs, Rank: Ace}), "Spades trump -> Mighty is NOT Ace of Clubs")

	g.Trump = Hearts
	assert.True(t, g.IsMighty(Card{Suit: Spades, Rank: Ace}), "Hearts trump -> Mighty is Ace of Spades")
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/game -run TestIsMighty`
Expected: FAIL (Mighty is NOT Ace of Diamonds under Spades trump).

- [ ] **Step 3: Write minimal implementation**
In `internal/game/rules.go`, replace `IsMighty` method content:
```go
func (g *Game) IsMighty(c Card) bool {
	// Usually Ace of Spades.
	// If Spades is Trump, Ace of Diamonds is Mighty.
	if g.Trump == Spades {
		return c.Suit == Diamonds && c.Rank == Ace
	}

	return c.Suit == Spades && c.Rank == Ace
}
```

- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v ./internal/game -run TestIsMighty`
Expected: PASS

- [ ] **Step 5: Commit**
```bash
git add internal/game/rules.go internal/game/rules_restrictions_test.go
git commit -m "fix(game): switch Mighty to Ace of Diamonds when Trump is Spades"
```

---

### Task 2: Player Helper Functions

**Files:**
- Modify: `internal/game/rules.go` (around line 390)
- Modify: `internal/game/rules_restrictions_test.go`

**Interfaces:**
- Produces: `HasMighty(g *Game) bool`, `HasNonTrumpMightyJoker(g *Game) bool`, `GetSuitCount(s Suit) int` on `*Player`.

- [ ] **Step 1: Write the failing test**

Append to `internal/game/rules_restrictions_test.go`:
```go
func TestPlayerHelpers(t *testing.T) {
	g := New("test")
	g.Trump = Hearts
	p := &Player{Hand: []Card{
		{Suit: Spades, Rank: Ace},   // Mighty
		{Suit: Hearts, Rank: Three}, // Trump
		{Suit: None, Rank: Joker},   // Joker
	}}

	assert.True(t, p.HasMighty(g))
	assert.False(t, p.HasNonTrumpMightyJoker(g), "Hand only has Trump, Mighty, Joker")
	assert.Equal(t, 1, p.GetSuitCount(Spades))

	p.Hand = append(p.Hand, Card{Suit: Clubs, Rank: Five})
	assert.True(t, p.HasNonTrumpMightyJoker(g))
	assert.Equal(t, 1, p.GetSuitCount(Clubs))
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/game -run TestPlayerHelpers`
Expected: FAIL (methods not defined).

- [ ] **Step 3: Write minimal implementation**

In `internal/game/rules.go`, append these to the `Player` helper section (around line 390, after `HasNonTrump`):
```go
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
```

- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v ./internal/game -run TestPlayerHelpers`
Expected: PASS

- [ ] **Step 5: Commit**
```bash
git add internal/game/rules.go internal/game/rules_restrictions_test.go
git commit -m "feat(game): add hand evaluation helpers for card restrictions"
```

---

### Task 3: Late-Game Special Card Forcing

**Files:**
- Modify: `internal/game/rules.go:260-264` (in `validatePlayCard`)
- Modify: `internal/game/rules_restrictions_test.go`

**Interfaces:**
- Consumes: `HasMighty`, `HasRank(Joker)`

- [ ] **Step 1: Write the failing test**

Append to `internal/game/rules_restrictions_test.go`:
```go
func TestLateGameSpecialForcing(t *testing.T) {
	g := New("test")
	g.Trump = Hearts
	g.Status = PhasePlaying
	p := &Player{ID: "p1", Hand: []Card{
		{Suit: Spades, Rank: Ace}, // Mighty
		{Suit: Clubs, Rank: Five},
	}}
	g.Players[0] = p
	g.CurrentTurn = 0
	g.Tricks = []Trick{{LeadSuit: Clubs}} // Active trick, 9th trick (since hand has 2 cards left)

	// Trying to follow suit with 2 cards left and holding Mighty MUST fail.
	err := g.ValidateMove("p1", MovePlayCard, PlayCardMove{Card: Card{Suit: Clubs, Rank: Five}})
	assert.ErrorContains(t, err, "must play mighty or joker")

	// Playing Mighty is allowed and forced.
	err = g.ValidateMove("p1", MovePlayCard, PlayCardMove{Card: Card{Suit: Spades, Rank: Ace}})
	assert.NoError(t, err)

	p.Hand = []Card{{Suit: Spades, Rank: Ace}, {Suit: None, Rank: Joker}, {Suit: Clubs, Rank: Five}}
	// 3 cards left, holding BOTH. Must play one.
	err = g.ValidateMove("p1", MovePlayCard, PlayCardMove{Card: Card{Suit: Clubs, Rank: Five}})
	assert.ErrorContains(t, err, "must play mighty or joker")
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/game -run TestLateGameSpecialForcing`
Expected: FAIL (plays Clubs Five successfully instead of returning an error).

- [ ] **Step 3: Write minimal implementation**

In `internal/game/rules.go`, inside `validatePlayCard`, locate the `// 1. Forced Play (Joker Called)` block (around line 264). Before it, insert:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v ./internal/game -run TestLateGameSpecialForcing`
Expected: PASS

- [ ] **Step 5: Commit**
```bash
git commit -am "feat(game): force early play of Mighty and Joker to prevent last-trick hoarding"
```

---

### Task 4: First Trick Restrictions (Opener & Follower)

**Files:**
- Modify: `internal/game/rules.go`
- Modify: `internal/game/rules_restrictions_test.go`

**Interfaces:**
- Consumes: `HasNonTrumpMightyJoker`, `GetSuitCount`

- [ ] **Step 1: Write the failing test**

Append to `internal/game/rules_restrictions_test.go`:
```go
func TestFirstTrickRestrictions(t *testing.T) {
	g := New("test")
	g.Trump = Hearts
	g.Status = PhasePlaying
	p1 := &Player{ID: "p1", Hand: []Card{{Suit: Hearts, Rank: Ace}, {Suit: Clubs, Rank: Five}}}
	g.Players[0] = p1
	g.CurrentTurn = 0
	
	// Opener leads Trick 1
	g.Tricks = append(g.Tricks, Trick{}) 
	
	// Opener tries to lead Trump
	err := g.ValidateMove("p1", MovePlayCard, PlayCardMove{Card: Card{Suit: Hearts, Rank: Ace}})
	assert.ErrorContains(t, err, "cannot lead trump on first trick")

	// Follower playing Mighty
	g.Tricks[0].LeadSuit = Spades
	g.Tricks[0].Cards = append(g.Tricks[0].Cards, PlayedCard{})
	
	p2 := &Player{ID: "p2", Hand: []Card{{Suit: Spades, Rank: Ace}, {Suit: Spades, Rank: Two}}}
	g.Players[1] = p2
	g.CurrentTurn = 1

	err = g.ValidateMove("p2", MovePlayCard, PlayCardMove{Card: Card{Suit: Spades, Rank: Ace}})
	assert.ErrorContains(t, err, "cannot play mighty on first trick unless it is your only card of the led suit")
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/game -run TestFirstTrickRestrictions`
Expected: FAIL (Opener might fail for different string, Follower will succeed or fail with wrong string).

- [ ] **Step 3: Write minimal implementation**

In `internal/game/rules.go`, inside `validatePlayCard`, under `// 2. Leading Rules`, **replace** the entire `if len(g.Tricks) == 1 { ... }` block inside `if len(t.Cards) == 0 {` with:

```go
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
```

Next, under `// 3. Following Suit`, inside `if card.Suit != lead { ... }`, **replace** the existing `if len(g.Tricks) == 1 && g.IsMighty(card) { ... }` block with:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v ./internal/game -run TestFirstTrickRestrictions`
Expected: PASS

- [ ] **Step 5: Commit**
```bash
git commit -am "feat(game): enforce strict first trick restrictions for opener and follower"
```
