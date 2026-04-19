Feature: User Authentication
  As a player
  I want to register and login
  So that I can have a persistent identity and track my stats

  Scenario: Successful Signup
    Given the game server is running
    When I sign up with username "tester1" and password "pass123" and email "tester1@example.com"
    Then the response status should be 201
    And the response should contain a valid user ID

  Scenario: Successful Login
    Given a user "tester2" exists with password "pass123"
    When I login with username "tester2" and password "pass123"
    Then the response status should be 200
    And the response should contain a valid JWT token
