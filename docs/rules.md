# Mighty Game Rules

## Introduction
Mighty is a high-stakes, point-trick card game featuring bidding and a "Mystery Friend" partner mechanic.

## Players and Cards
- **Players**: 5 players (Standard).
- **Deck**: 52 cards + 1 Joker (53 total).
- **Ranks**: A (High) → 2 (Low).
- **Points**: A, K, Q, J, and 10 of any suit are "Point Cards". There are 20 total point cards in the deck.

## Special Cards (The Magic Cards)

### 1. The Mighty
The strongest card in the game. It wins any trick it is played in.
- **Normal**: Ace of Spades (♠A).
- **If Spades are Trump**: Ace of Clubs (♣A).

### 2. The Joker
The second strongest card. It wins any trick unless:
- The Mighty is played in the same trick.
- It is the **First Trick** or the **Last Trick** of the hand (in which case it loses all power).
- The **Joker Caller** was led and called for the Joker.

### 3. The Joker Caller (The Ripper)
A nemesis for the Joker.
- **Normal**: Three of Clubs (♣3).
- **If Clubs are Trump**: Three of Spades (♠3).
- **Rule**: If led, the player can choose to "Call the Joker". The player holding the Joker **MUST** play it, and the Joker loses all power for that trick.
- **Exception**: If the Joker holder also has the Mighty, they can play the Mighty instead to win the trick and save their Joker.

## The Game Flow

### 1. Bidding Phase
Players bid for the contract, specifying the number of tricks (3-10) and the trump suit (or No-Trump).
- Minimum bid: 3.
- No-Trump bids beat suit bids of the same level. Suit bids are ranked: Clubs < Diamonds < Hearts < Spades.
- Bidding ends after 4 consecutive passes. The winner becomes the **Declarer**.

### 2. Exchanging Phase (The Kitty)
- 3 cards are dealt face-down as the "Kitty".
- The Declarer takes the Kitty and then discards 3 cards of their choice back to their score pile.

### 3. Calling the Friend
The Declarer calls out a specific card (e.g., "Ace of Hearts").
- The player holding that card becomes the **Partner**.
- The Partner remains secret until the called card is played.
- **No Friend**: The Declarer can choose to play alone for doubled points.

### 4. Playing Phase
- The Declarer leads the first trick.
- **Rule**: No trump can be led on the first trick unless the player has only trumps.
- **Rule**: The Mighty cannot be played on the first trick unless the player cannot follow the lead suit.
- Players must follow the lead suit if possible.

## Scoring (Official Mighty)

Scores are zero-sum: they add up to zero across all five players. `P` is the number
of the 20 scoring cards the declarer's team captured. Bids are entered on a 3–10
scale, mapping to a scoring-card target of `bid + 10` (13–20).

- **Success** (`P ≥ bid + 10`): `S = 2×(bid − 3) + (P − (bid + 10))`.
- **Failure** (`P < bid + 10`): `S = (bid + 10) − P`.
- **Distribution** — success: declarer **+2S**, partner **+S**, each opponent **−S**;
  failure reverses every sign. Playing alone: declarer **±4S**, each opponent **∓S**.
- **Multipliers** (each ×2, stacking):
    - **Run**: team captured all 20 scoring cards.
    - **Back-run**: opponents captured ≥ 11 scoring cards.
    - **No-Trump**.
    - **No-Friend**: announced solo only (not a secret solo).
- If the bid is the minimum (3) and the team takes exactly 13 points, `S = 0` and
  there is no payment.
