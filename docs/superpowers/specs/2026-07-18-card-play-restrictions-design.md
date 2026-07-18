# Mighty Game Card Play Restrictions Design

## 1. Overview
This design implements several missing card play restrictions in the Mighty game engine, specifically focusing on the first trick, the last trick, and correcting the default behavior of the Mighty card.

## 2. Mighty Default Correction
**Bug**: The engine incorrectly switches the Mighty card to the Ace of Clubs when the trump suit is Spades.
**Fix**: Update the `IsMighty(c Card)` method in `internal/game/rules.go` to default to the **Ace of Spades**, and properly switch to the **Ace of Diamonds** if the trump suit is Spades. 
*(Note: The Ripper logic already correctly switches to the 3 of Spades when the trump suit is Clubs, so no changes are needed there).*

## 3. First Trick Restrictions
During the first trick of the game (`len(g.Tricks) == 1`), specific restrictions apply to prevent early use of powerful cards.

### 3.1. Trick Opener (Leading the trick)
- **No Joker**: The opener cannot lead with the Joker.
- **No Mighty**: The opener cannot lead with the Mighty.
- **No Trump**: The opener cannot lead with a Trump suit card.
  - **Exception**: If the opener's hand consists *entirely* of Trump cards, the Mighty, and the Joker, they are permitted to lead with a Trump suit card (but still cannot lead the Mighty or Joker).

### 3.2. Following Players
- **No Joker**: Following players cannot play the Joker.
- **No Mighty**: Following players cannot play the Mighty.
  - **Exception**: If the trick opener led the base suit of the Mighty (e.g., Ace of Spades or Diamonds), AND the Mighty is the *only* card the player has of that suit, they are permitted to play the Mighty.
- **No Trump**: Following players cannot play a Trump card.
  - **Exception**: If the player is void in the led suit, they are permitted to play a Trump card (standard ruffing).

## 4. Late-Game Special Card Forcing
Players are strictly prohibited from playing the Mighty or the Joker during the final (10th) trick. To enforce this, the engine will force players to play these cards earlier.

This is determined by checking the number of cards remaining in a player's hand during their turn:
- **3 Cards Left (Trick 8)**: If the player holds *both* the Mighty and the Joker, they must play one of them.
- **2 Cards Left (Trick 9)**: If the player holds the Mighty or the Joker, they must play it.
- **1 Card Left (Trick 10)**: Due to the rules above, no player will enter the final trick holding the Mighty or Joker.

**Override Rule**: The forced early play of the Mighty or Joker *overrides* the standard "must follow suit" rule. Even if the player holds a card matching the led suit, they must play the Mighty or Joker to satisfy the early play constraint.

## 5. Implementation Strategy
All modifications will be contained within `internal/game/rules.go` and its corresponding test file `internal/game/rules_test.go`.

1. **`IsMighty`**: Modify the suit check for Spades trump.
2. **`validatePlayCard`**:
   - Inject the "Early Play" forcing logic. If the conditions are met (2 or 3 cards left) and the player attempts to play a non-special card, return an `ErrInvalidMove`.
   - Inject the "First Trick" logic. If `len(g.Tricks) == 1`, enforce the opener and follower constraints.
3. **Tests**: Add comprehensive unit tests verifying:
   - Mighty switches to Ace of Diamonds when Trump is Spades.
   - Trick opener cannot lead Trump, Mighty, or Joker on Trick 1.
   - Follower cannot play Mighty or Joker on Trick 1 (with valid exceptions).
   - Early play forcing overrides the "follow suit" rule on the 9th trick.
