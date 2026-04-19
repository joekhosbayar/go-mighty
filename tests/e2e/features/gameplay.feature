Feature: Mighty Gameplay
  As a group of players
  We want to play a full game of Mighty
  So that we can experience the thrill of the "Mystery Friend"

  Scenario: End-to-End Game Session
    Given 5 authenticated players: "Alice", "Bob", "Carol", "Dave", "Eve"
    And "Alice" creates a game "mighty-match-999"
    When all 5 players join the game "mighty-match-999" in order:
      | name  | seat |
      | Alice | 0    |
      | Bob   | 1    |
      | Carol | 2    |
      | Dave  | 3    |
      | Eve   | 4    |
    Then the game "mighty-match-999" status should be "bidding"
    And each player should have 10 cards in their hand
    
    When "Alice" bids 13 "spades"
    And "Bob", "Carol", "Dave", and "Eve" pass
    Then "Alice" should be the declarer
    And the game "mighty-match-999" status should be "exchanging"
    
    When "Alice" discards 3 cards:
      | suit   | rank |
      | hearts | 2    |
      | hearts | 3    |
      | hearts | 4    |
    Then the game "mighty-match-999" status should be "calling"
    
    When "Alice" calls the "Ace of Hearts" as the friend
    Then the game "mighty-match-999" status should be "playing"
    
    When 10 tricks are played through the WebSocket
    Then the game "mighty-match-999" status should be "finished"
    And final scores should be calculated correctly
