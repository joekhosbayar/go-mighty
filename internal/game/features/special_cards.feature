Feature: Special Card Mechanics
  As a group of players
  We want to verify the unique behaviors of the Mighty, Joker, and Joker Caller
  So that the high-stakes strategy remains intact

  Background:
    Given 5 authenticated players: "Alice", "Bob", "Carol", "Dave", "Eve"
    And "Alice" creates a high-stakes game "special-test-888"
    When "Alice", "Bob", "Carol", "Dave", "Eve" join the game "special-test-888"
    And "Alice" wins a "7 diamonds" contract
    And "Alice" discards 3 cards
    And "Alice" calls the "Ace of Hearts" as the friend
    Then the game "special-test-888" status should be "playing"

  Scenario: Joker beats Ace of Trump
    Given it is Trick 5
    And the trump suit is "spades"
    When "Bob" leads the "King of Hearts"
    And "Carol" plays the "Ace of Spades" (Ace of Trump)
    And "Dave" plays the "Joker"
    Then the "Joker" should win the trick
    And "Dave" should be the next turn

  Scenario: Mighty beats Joker
    Given it is Trick 3
    And the trump suit is "hearts"
    # Mighty is Ace of Spades
    When "Bob" leads the "King of Clubs"
    And "Carol" plays the "Joker"
    And "Dave" plays the "Ace of Spades" (Mighty)
    Then the "Ace of Spades" should win the trick
    And "Dave" should be the next turn

  Scenario: Joker Caller forces Joker and strips its power
    Given it is Trick 4
    And the trump suit is "hearts"
    # Joker Caller is 3 of Clubs
    And "Bob" has the "Joker"
    When "Alice" leads the "3 of Clubs" and calls out the Joker
    And "Bob" plays the "Joker"
    And "Carol" plays the "2 of Clubs"
    Then the "3 of Clubs" should win the trick
    And "Alice" should be the next turn

  Scenario: Mighty saves Joker from Joker Caller
    Given it is Trick 2
    And the trump suit is "hearts"
    And "Bob" has both the "Joker" and the "Ace of Spades" (Mighty)
    When "Alice" leads the "3 of Clubs" and calls out the Joker
    And "Bob" plays the "Ace of Spades" (Mighty)
    Then the "Ace of Spades" should win the trick
    And "Bob" should be the next turn
    And "Bob" should still have the "Joker" in hand

  Scenario: First Hand Lead Restriction
    Given it is Trick 1
    And the trump suit is "spades"
    And "Alice" has the "King of Spades" (Trump) and "2 of Hearts" (Non-Trump)
    When "Alice" attempts to lead the "King of Spades"
    Then the move should be rejected as "cannot lead trump on first trick"

  Scenario: First Hand Mighty Play Restriction
    Given it is Trick 1
    And "Alice" leads the "King of Hearts"
    And "Bob" has the "Ace of Spades" (Mighty) and "2 of Hearts" (Lead Suit)
    When "Bob" attempts to play the "Ace of Spades"
    Then the move should be rejected as "cannot play mighty on first trick if you can follow suit"
