Feature: Game Lobby
  As an authenticated player
  I want to create and find games
  So that I can play with others

  Scenario: Create a new game
    Given I am logged in as "creator1"
    When I create a new game with ID "gherkin-game-1"
    Then the response status should be 200
    And the game "gherkin-game-1" should exist

  Scenario: List waiting games
    Given I am logged in as "browser1"
    And there are 2 games waiting for players
    When I list games with status "waiting"
    Then the response status should be 200
    And I should see at least 2 games in the list
