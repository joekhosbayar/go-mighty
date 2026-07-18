Feature: Mystery Friend
  The declarer's called card reveals the partner when played,
  and no_friend lets the declarer play alone.

  Scenario: Partner is revealed when the called card is played
    Given 5 authenticated players: "Ann", "Ben", "Cid", "Dot", "Eli"
    And "Ann" creates a high-stakes game "friend-1"
    When "Ann" joins seat 0 of game "friend-1"
    And "Ben" joins seat 1 of game "friend-1"
    And "Cid" joins seat 2 of game "friend-1"
    And "Dot" joins seat 3 of game "friend-1"
    And "Eli" joins seat 4 of game "friend-1"
    Then the game "friend-1" status should be "bidding"
    When "Ann" bids 5 "spades"
    And "Ben" passes
    And "Cid" passes
    And "Dot" passes
    And "Eli" passes
    Then the game "friend-1" status should be "exchanging"
    When "Ann" discards 3 least powerful cards
    And "Ann" calls the "Ace of Hearts" as the friend
    Then the game "friend-1" status should be "playing"
    When all remaining tricks are played out legally
    Then the game "friend-1" status should be "finished"
    And the partner seat should be unrevealed or match whoever played the called card
    And the final scores should follow the declarer-partner split

  Scenario: Declarer plays alone with no friend
    Given 5 authenticated players: "Fay", "Gus", "Hal", "Ivy", "Jon"
    And "Fay" creates a high-stakes game "friend-2"
    When "Fay" joins seat 0 of game "friend-2"
    And "Gus" joins seat 1 of game "friend-2"
    And "Hal" joins seat 2 of game "friend-2"
    And "Ivy" joins seat 3 of game "friend-2"
    And "Jon" joins seat 4 of game "friend-2"
    Then the game "friend-2" status should be "bidding"
    When "Fay" bids 5 "spades"
    And "Gus" passes
    And "Hal" passes
    And "Ivy" passes
    And "Jon" passes
    Then the game "friend-2" status should be "exchanging"
    When "Fay" discards 3 least powerful cards
    And "Fay" declares no friend
    Then the game "friend-2" status should be "playing"
    And the game should have no friend
    When all remaining tricks are played out legally
    Then the game "friend-2" status should be "finished"
    And the final scores should follow the declarer-partner split
