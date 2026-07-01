Feature: WebSocket Authentication and Real-Time Events
  As a connected client
  I want to authenticate over WebSocket and receive real-time game events
  So that I can stay in sync with the game without polling

  Scenario: WebSocket rejects an invalid auth token
    Given the game server is running
    And I am logged in as "ws_creator"
    And I create a new game with ID "ws-auth-test"
    When a WebSocket client connects to game "ws-auth-test" with an invalid token
    Then the WebSocket should receive an error containing "unauthorized"

  Scenario: Subscriber receives a player_joined event in real time
    Given the game server is running
    And I am logged in as "ws_host"
    And I create a new game with ID "ws-realtime-test"
    When a WebSocket subscriber connects to game "ws-realtime-test" as "ws_host"
    And I am logged in as "ws_joiner"
    And "ws_joiner" joins seat 1 of game "ws-realtime-test"
    Then the subscriber should receive a "player_joined" event within 5 seconds
