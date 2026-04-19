Feature: Full Mighty Game Marathon
  As a group of five elite players
  We want to play an entire game of Mighty from start to finish
  So that we can verify the core engine, the mystery friend logic, and the final scoring

  Scenario: The 800-Point Journey
    Given 5 authenticated players: "Alice", "Bob", "Carol", "Dave", "Eve"
    And "Alice" creates a high-stakes game "marathon-999"
    
    # 1. Joining Phase
    When "Alice" joins seat 0 of game "marathon-999"
    And "Bob" joins seat 1 of game "marathon-999"
    And "Carol" joins seat 2 of game "marathon-999"
    And "Dave" joins seat 3 of game "marathon-999"
    And "Eve" joins seat 4 of game "marathon-999"
    Then the game "marathon-999" status should be "bidding"
    And all players should have 10 cards
    
    # 2. Bidding Phase
    When "Alice" bids 14 "spades"
    And "Bob" passes
    And "Carol" bids 15 "spades"
    And "Dave" passes
    And "Eve" passes
    And "Alice" bids 16 "spades"
    And "Carol" passes
    Then "Alice" should be the declarer with a bid of 16 "spades"
    And the game "marathon-999" status should be "exchanging"
    
    # 3. Kitty / Exchanging Phase
    And "Alice" should have 13 cards in hand
    When "Alice" discards 3 least powerful cards
    Then the game "marathon-999" status should be "calling"
    And "Alice" should have 10 cards in hand
    
    # 4. Calling the Friend
    When "Alice" calls the "Ace of Diamonds" as the friend
    Then the game "marathon-999" status should be "playing"
    And the trump suit should be "spades"
    
    # 5. The 10-Trick Marathon
    When "Alice" leads the first trick
    And all players play out Trick 1 legally
    Then Trick 1 should have a winner
    
    When the winner of Trick 1 leads Trick 2
    And all players play out Trick 2 legally
    Then Trick 2 should have a winner
    
    When the winner of Trick 2 leads Trick 3
    And all players play out Trick 3 legally
    
    When the winner of Trick 3 leads Trick 4
    And all players play out Trick 4 legally
    
    When the winner of Trick 4 leads Trick 5
    And all players play out Trick 5 legally
    
    When the winner of Trick 5 leads Trick 6
    And all players play out Trick 6 legally
    
    When the winner of Trick 6 leads Trick 7
    And all players play out Trick 7 legally
    
    When the winner of Trick 7 leads Trick 8
    And all players play out Trick 8 legally
    
    When the winner of Trick 8 leads Trick 9
    And all players play out Trick 9 legally
    
    When the winner of Trick 9 leads Trick 10
    And all players play out Trick 10 legally
    
    # 6. Final Validation
    Then the game "marathon-999" status should be "finished"
    And the total number of tricks won should be 10
    And the final scores should be calculated and non-zero
