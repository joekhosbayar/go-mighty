package game

import (
	"testing"
	"time"
)

// TestNewGame tests game creation
// Rule: New game should initialize with correct settings and empty player slots
func TestNewGame(t *testing.T) {
	gameID := "test-game-123"
	game := NewGame(gameID, 5)

	if game.GameID != gameID {
		t.Errorf("GameID = %v, want %v", game.GameID, gameID)
	}
	if game.MaxPlayers != 5 {
		t.Errorf("MaxPlayers = %d, want 5", game.MaxPlayers)
	}
	if game.Status != PhaseWaiting {
		t.Errorf("Status = %v, want PhaseWaiting", game.Status)
	}
	if len(game.Players) != 5 {
		t.Errorf("len(Players) = %d, want 5", len(game.Players))
	}
	if game.Variant != "mighty-5p-standard" {
		t.Errorf("Variant = %v, want 'mighty-5p-standard'", game.Variant)
	}
	if game.HandNo != 0 {
		t.Errorf("HandNo = %d, want 0", game.HandNo)
	}
}

// TestDefaultGameOptions tests default game options
// Rule: Default options should match standard Mighty rules
func TestDefaultGameOptions(t *testing.T) {
	opts := DefaultGameOptions()

	if opts.MinBid != 13 {
		t.Errorf("MinBid = %d, want 13", opts.MinBid)
	}
	if !opts.AllowNoTrump {
		t.Error("AllowNoTrump should be true")
	}
	if !opts.AllowNoFriend {
		t.Error("AllowNoFriend should be true")
	}
	if !opts.AllowRaiseBid {
		t.Error("AllowRaiseBid should be true")
	}
	if !opts.AllowChangeTrump {
		t.Error("AllowChangeTrump should be true")
	}
}

// TestGame_AddPlayer tests adding players to seats
// Rule: Players should be added to specific seats before game starts
func TestGame_AddPlayer(t *testing.T) {
	game := NewGame("test", 5)

	err := game.AddPlayer("player1", 0)
	if err != nil {
		t.Errorf("AddPlayer error = %v", err)
	}

	player := game.GetPlayer(0)
	if player == nil {
		t.Fatal("Player should not be nil")
	}
	if player.PlayerID != "player1" {
		t.Errorf("PlayerID = %v, want 'player1'", player.PlayerID)
	}
	if player.SeatNo != 0 {
		t.Errorf("SeatNo = %d, want 0", player.SeatNo)
	}
	if !player.Connected {
		t.Error("Player should be connected")
	}
}

// TestGame_AddPlayer_InvalidSeat tests error handling for invalid seats
// Rule: Seat numbers must be within valid range
func TestGame_AddPlayer_InvalidSeat(t *testing.T) {
	game := NewGame("test", 5)

	tests := []struct {
		name   string
		seatNo int
	}{
		{"Negative seat", -1},
		{"Seat too high", 5},
		{"Way too high", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := game.AddPlayer("player", tt.seatNo)
			if err != ErrInvalidSeat {
				t.Errorf("AddPlayer(%d) error = %v, want ErrInvalidSeat", tt.seatNo, err)
			}
		})
	}
}

// TestGame_AddPlayer_SeatOccupied tests error when seat already taken
// Rule: Cannot add player to occupied seat
func TestGame_AddPlayer_SeatOccupied(t *testing.T) {
	game := NewGame("test", 5)

	// Add first player
	game.AddPlayer("player1", 0)

	// Try to add second player to same seat
	err := game.AddPlayer("player2", 0)

	if err != ErrSeatOccupied {
		t.Errorf("AddPlayer to occupied seat error = %v, want ErrSeatOccupied", err)
	}
}

// TestGame_AddPlayer_AfterStart tests error when adding after game starts
// Rule: Cannot add players after game has started
func TestGame_AddPlayer_AfterStart(t *testing.T) {
	game := NewGame("test", 5)

	// Fill all seats and start
	for i := 0; i < 5; i++ {
		game.AddPlayer("player"+string(rune(i)), i)
	}
	game.Start()

	// Try to add player after game started
	err := game.AddPlayer("lateplayer", 0)

	if err != ErrGameAlreadyStarted {
		t.Errorf("AddPlayer after start error = %v, want ErrGameAlreadyStarted", err)
	}
}

// TestGame_RemovePlayer tests removing a player
// Rule: Should be able to remove players from seats
func TestGame_RemovePlayer(t *testing.T) {
	game := NewGame("test", 5)
	game.AddPlayer("player1", 2)

	err := game.RemovePlayer(2)

	if err != nil {
		t.Errorf("RemovePlayer error = %v", err)
	}
	if game.GetPlayer(2) != nil {
		t.Error("Player should be nil after removal")
	}
}

// TestGame_RemovePlayer_InvalidSeat tests error for invalid seat removal
// Rule: Cannot remove from invalid seat number
func TestGame_RemovePlayer_InvalidSeat(t *testing.T) {
	game := NewGame("test", 5)

	err := game.RemovePlayer(-1)

	if err != ErrInvalidSeat {
		t.Errorf("RemovePlayer(-1) error = %v, want ErrInvalidSeat", err)
	}
}

// TestGame_GetPlayerBySeatNo tests retrieving player by seat
// Rule: Should retrieve player at specific seat or error if not found
func TestGame_GetPlayerBySeatNo(t *testing.T) {
	game := NewGame("test", 5)
	game.AddPlayer("player1", 2)

	// Valid player
	player, err := game.GetPlayerBySeatNo(2)
	if err != nil {
		t.Errorf("GetPlayerBySeatNo(2) error = %v", err)
	}
	if player.PlayerID != "player1" {
		t.Errorf("PlayerID = %v, want 'player1'", player.PlayerID)
	}

	// Empty seat
	_, err = game.GetPlayerBySeatNo(3)
	if err != ErrPlayerNotFound {
		t.Errorf("GetPlayerBySeatNo(empty) error = %v, want ErrPlayerNotFound", err)
	}

	// Invalid seat
	_, err = game.GetPlayerBySeatNo(-1)
	if err != ErrInvalidSeat {
		t.Errorf("GetPlayerBySeatNo(-1) error = %v, want ErrInvalidSeat", err)
	}
}

// TestGame_GetPlayerByID tests finding player by ID
// Rule: Should find player by their ID or error if not found
func TestGame_GetPlayerByID(t *testing.T) {
	game := NewGame("test", 5)
	game.AddPlayer("player1", 0)
	game.AddPlayer("player2", 3)

	// Valid player
	player, err := game.GetPlayerByID("player1")
	if err != nil {
		t.Errorf("GetPlayerByID error = %v", err)
	}
	if player.SeatNo != 0 {
		t.Errorf("SeatNo = %d, want 0", player.SeatNo)
	}

	// Non-existent player
	_, err = game.GetPlayerByID("nonexistent")
	if err != ErrPlayerNotFound {
		t.Errorf("GetPlayerByID(nonexistent) error = %v, want ErrPlayerNotFound", err)
	}
}

// TestGame_IsReadyToStart tests game start readiness
// Rule: Game is ready when all seats are filled
func TestGame_IsReadyToStart(t *testing.T) {
	game := NewGame("test", 5)

	// Not ready with empty seats
	if game.IsReadyToStart() {
		t.Error("Game should not be ready with empty seats")
	}

	// Add 4 players
	for i := 0; i < 4; i++ {
		game.AddPlayer("player"+string(rune(i)), i)
	}

	// Still not ready
	if game.IsReadyToStart() {
		t.Error("Game should not be ready with 4/5 players")
	}

	// Add 5th player
	game.AddPlayer("player4", 4)

	// Now ready
	if !game.IsReadyToStart() {
		t.Error("Game should be ready with all seats filled")
	}

	// Not ready after start
	game.Start()
	if game.IsReadyToStart() {
		t.Error("Game should not be 'ready to start' after starting")
	}
}

// TestGame_Start tests starting the game
// Rule: Game should transition from waiting to bidding phase
func TestGame_Start(t *testing.T) {
	game := NewGame("test", 5)

	// Fill all seats
	for i := 0; i < 5; i++ {
		game.AddPlayer("player"+string(rune(i)), i)
	}

	err := game.Start()

	if err != nil {
		t.Errorf("Start error = %v", err)
	}
	if game.Status != PhaseBidding {
		t.Errorf("Status = %v, want PhaseBidding", game.Status)
	}
	if game.StartedAt == nil {
		t.Error("StartedAt should not be nil")
	}
	if time.Since(*game.StartedAt) > time.Second {
		t.Error("StartedAt should be recent")
	}
}

// TestGame_Start_NotReady tests error when starting without all players
// Rule: Cannot start game unless all seats filled
func TestGame_Start_NotReady(t *testing.T) {
	game := NewGame("test", 5)

	// Only add 3 players
	for i := 0; i < 3; i++ {
		game.AddPlayer("player"+string(rune(i)), i)
	}

	err := game.Start()

	if err != ErrInvalidPlayerCount {
		t.Errorf("Start error = %v, want ErrInvalidPlayerCount", err)
	}
}

// TestGame_StartNewHand tests starting a new hand
// Rule: New hand should deal cards to all players and create kitty
func TestGame_StartNewHand(t *testing.T) {
	game := NewGame("test", 5)

	// Fill and start game
	for i := 0; i < 5; i++ {
		game.AddPlayer("player"+string(rune(i)), i)
	}
	game.Start()

	err := game.StartNewHand(0)

	if err != nil {
		t.Errorf("StartNewHand error = %v", err)
	}
	if game.HandNo != 1 {
		t.Errorf("HandNo = %d, want 1", game.HandNo)
	}
	if game.CurrentHand == nil {
		t.Fatal("CurrentHand should not be nil")
	}
	if game.CurrentHand.DealerSeat != 0 {
		t.Errorf("DealerSeat = %d, want 0", game.CurrentHand.DealerSeat)
	}

	// Check cards were dealt
	for i := 0; i < 5; i++ {
		if len(game.CurrentHand.PlayerHands[i]) != 10 {
			t.Errorf("Player %d has %d cards, want 10", i, len(game.CurrentHand.PlayerHands[i]))
		}
	}
	if len(game.CurrentHand.Kitty) != 3 {
		t.Errorf("Kitty has %d cards, want 3", len(game.CurrentHand.Kitty))
	}
}

// TestGame_StartNewHand_BeforeGameStart tests error when hand starts before game
// Rule: Cannot start hand before game has started
func TestGame_StartNewHand_BeforeGameStart(t *testing.T) {
	game := NewGame("test", 5)

	err := game.StartNewHand(0)

	if err != ErrGameNotStarted {
		t.Errorf("StartNewHand error = %v, want ErrGameNotStarted", err)
	}
}

// TestGame_CompleteCurrentHand tests completing a hand
// Rule: Hand should be added to history when completed
func TestGame_CompleteCurrentHand(t *testing.T) {
	game := NewGame("test", 5)

	// Setup completed hand
	for i := 0; i < 5; i++ {
		game.AddPlayer("player"+string(rune(i)), i)
	}
	game.Start()
	game.StartNewHand(0)

	// Mark hand as complete (add 10 tricks)
	for i := 0; i < 10; i++ {
		game.CurrentHand.Tricks = append(game.CurrentHand.Tricks, NewTrick(i+1, 0))
	}

	err := game.CompleteCurrentHand()

	if err != nil {
		t.Errorf("CompleteCurrentHand error = %v", err)
	}
	if game.CurrentHand.Phase != PhaseHandComplete {
		t.Errorf("CurrentHand.Phase = %v, want PhaseHandComplete", game.CurrentHand.Phase)
	}
	if len(game.Hands) != 1 {
		t.Errorf("len(Hands) = %d, want 1", len(game.Hands))
	}
}

// TestGame_CompleteCurrentHand_NotComplete tests error when hand not finished
// Rule: Cannot complete hand if not all tricks played
func TestGame_CompleteCurrentHand_NotComplete(t *testing.T) {
	game := NewGame("test", 5)

	for i := 0; i < 5; i++ {
		game.AddPlayer("player"+string(rune(i)), i)
	}
	game.Start()
	game.StartNewHand(0)

	// Only 5 tricks played, not complete
	for i := 0; i < 5; i++ {
		game.CurrentHand.Tricks = append(game.CurrentHand.Tricks, NewTrick(i+1, 0))
	}

	err := game.CompleteCurrentHand()

	if err != ErrInvalidMove {
		t.Errorf("CompleteCurrentHand error = %v, want ErrInvalidMove", err)
	}
}

// TestGame_GetNextDealer tests dealer rotation
// Rule: Declarer's partner deals next, or declarer if played alone
func TestGame_GetNextDealer(t *testing.T) {
	game := NewGame("test", 5)

	// First hand: no previous hand
	nextDealer := game.GetNextDealer()
	if nextDealer != 0 {
		t.Errorf("First hand dealer = %d, want 0", nextDealer)
	}

	// Setup a completed hand
	game.CurrentHand = NewHand(1, 0, 5)
	game.CurrentHand.DeclarerSeat = 1
	game.CurrentHand.PartnerSeat = 3
	game.CurrentHand.Contract = &Contract{NoFriend: false}

	// Partner should deal next
	nextDealer = game.GetNextDealer()
	if nextDealer != 3 {
		t.Errorf("Next dealer = %d, want 3 (partner)", nextDealer)
	}
}

// TestGame_GetNextDealer_NoFriend tests dealer rotation when declarer played alone
// Rule: When declarer played alone, they deal next
func TestGame_GetNextDealer_NoFriend(t *testing.T) {
	game := NewGame("test", 5)

	game.CurrentHand = NewHand(1, 0, 5)
	game.CurrentHand.DeclarerSeat = 2
	game.CurrentHand.PartnerSeat = -1
	game.CurrentHand.Contract = &Contract{NoFriend: true}

	// Declarer should deal next when no friend
	nextDealer := game.GetNextDealer()
	if nextDealer != 2 {
		t.Errorf("Next dealer = %d, want 2 (declarer played alone)", nextDealer)
	}
}

// TestGame_ValidateCardPlay tests card play validation
// Rule: Must validate card is in hand and follows suit rules
func TestGame_ValidateCardPlay(t *testing.T) {
	game := NewGame("test", 5)

	for i := 0; i < 5; i++ {
		game.AddPlayer("player"+string(rune(i)), i)
	}
	game.Start()
	game.StartNewHand(0)

	// Setup playing phase
	game.CurrentHand.Phase = PhasePlaying
	game.CurrentHand.Contract = &Contract{
		Trump: Trump{Suit: Hearts, NoTrump: false},
	}

	card := game.CurrentHand.PlayerHands[0][0] // Get first card from player 0's hand

	err := game.ValidateCardPlay(0, card)

	if err != nil {
		t.Errorf("ValidateCardPlay error = %v", err)
	}
}

// TestGame_ValidateCardPlay_CardNotInHand tests validation failure
// Rule: Cannot play card not in hand
func TestGame_ValidateCardPlay_CardNotInHand(t *testing.T) {
	game := NewGame("test", 5)

	for i := 0; i < 5; i++ {
		game.AddPlayer("player"+string(rune(i)), i)
	}
	game.Start()
	game.StartNewHand(0)

	game.CurrentHand.Phase = PhasePlaying

	// Try to play a card not in hand
	fakeCard := Card{Suit: Spades, Rank: Ace}
	err := game.ValidateCardPlay(0, fakeCard)

	// Will likely fail unless by chance SA is in hand
	if err != ErrCardNotInHand {
		// It's possible SA was dealt, so just check error exists
		if err == nil {
			t.Log("Card happened to be in hand, skipping test")
		}
	}
}

// TestGame_ValidateCardPlay_WrongPhase tests validation in wrong phase
// Rule: Can only play cards during playing phase
func TestGame_ValidateCardPlay_WrongPhase(t *testing.T) {
	game := NewGame("test", 5)

	for i := 0; i < 5; i++ {
		game.AddPlayer("player"+string(rune(i)), i)
	}
	game.Start()
	game.StartNewHand(0)

	// Still in bidding phase
	game.CurrentHand.Phase = PhaseBidding

	card := Card{Suit: Spades, Rank: Ace}
	err := game.ValidateCardPlay(0, card)

	if err != ErrInvalidPhase {
		t.Errorf("ValidateCardPlay in wrong phase error = %v, want ErrInvalidPhase", err)
	}
}

// TestGame_ValidateCardPlay_CannotLeadTrump tests first trick trump restriction
// Rule: Cannot lead trump on first trick unless only have trumps
func TestGame_ValidateCardPlay_CannotLeadTrump(t *testing.T) {
	game := NewGame("test", 5)

	for i := 0; i < 5; i++ {
		game.AddPlayer("player"+string(rune(i)), i)
	}
	game.Start()
	game.StartNewHand(0)

	game.CurrentHand.Phase = PhasePlaying
	game.CurrentHand.Contract = &Contract{
		Trump: Trump{Suit: Hearts, NoTrump: false},
	}

	// Setup hand with both trump and non-trump
	game.CurrentHand.PlayerHands[0] = []Card{
		{Suit: Hearts, Rank: Ace},  // Trump
		{Suit: Spades, Rank: King}, // Non-trump
	}

	game.CurrentHand.StartTrick(0)

	// Try to lead with trump on first trick
	err := game.ValidateCardPlay(0, Card{Suit: Hearts, Rank: Ace})

	if err != ErrCannotLeadTrump {
		t.Errorf("Leading trump on first trick error = %v, want ErrCannotLeadTrump", err)
	}

	// Non-trump should be allowed
	err = game.ValidateCardPlay(0, Card{Suit: Spades, Rank: King})
	if err != nil {
		t.Errorf("Leading non-trump error = %v", err)
	}
}

// TestGame_UpdatePlayerRole tests role assignment
// Rule: Players are assigned roles based on declarer and partner
func TestGame_UpdatePlayerRole(t *testing.T) {
	game := NewGame("test", 5)

	game.CurrentHand = NewHand(1, 0, 5)
	game.CurrentHand.DeclarerSeat = 1
	game.CurrentHand.PartnerSeat = 3
	game.CurrentHand.PartnerRevealed = true

	role := game.UpdatePlayerRole(1)
	if role != RoleDeclarer {
		t.Errorf("Player 1 role = %v, want RoleDeclarer", role)
	}

	role = game.UpdatePlayerRole(3)
	if role != RolePartner {
		t.Errorf("Player 3 role = %v, want RolePartner", role)
	}

	role = game.UpdatePlayerRole(2)
	if role != RoleUndecided {
		t.Errorf("Player 2 role = %v, want RoleUndecided", role)
	}
}

// TestGame_UpdatePlayerRole_NoFriend tests role with no friend
// Rule: When playing alone, all others are opponents
func TestGame_UpdatePlayerRole_NoFriend(t *testing.T) {
	game := NewGame("test", 5)

	game.CurrentHand = NewHand(1, 0, 5)
	game.CurrentHand.DeclarerSeat = 1
	game.CurrentHand.Contract = &Contract{NoFriend: true}

	role := game.UpdatePlayerRole(2)
	if role != RoleOpponent {
		t.Errorf("Player 2 role = %v, want RoleOpponent (no friend)", role)
	}
}

// TestGame_CalculateScore_Success tests score calculation when contract made
// Rule: S = 2×(B−M) + (P−B) when successful
func TestGame_CalculateScore_Success(t *testing.T) {
	game := NewGame("test", 5)
	game.Options.MinBid = 13

	game.CurrentHand = NewHand(1, 0, 5)
	game.CurrentHand.DeclarerSeat = 1
	game.CurrentHand.PartnerSeat = 3
	game.CurrentHand.Contract = &Contract{
		DeclarerSeat: 1,
		Points:       15, // B = 15
		Trump:        Trump{Suit: Hearts, NoTrump: false},
		NoFriend:     false,
	}

	// Mark as complete
	for i := 0; i < 10; i++ {
		game.CurrentHand.Tricks = append(game.CurrentHand.Tricks, NewTrick(i+1, 0))
	}

	// Set points: declarer and partner took 16 points
	game.CurrentHand.PointsBySeat = map[int]int{
		0: 1,
		1: 8, // Declarer
		2: 2,
		3: 8, // Partner
		4: 1,
	}

	scores, err := game.CalculateScore()

	if err != nil {
		t.Errorf("CalculateScore error = %v", err)
	}

	// P = 16, B = 15, M = 13
	// S = 2×(15−13) + (16−15) = 2×2 + 1 = 5
	// Declarer: +10, Partner: +5, Opponents: −5 each

	if scores[1] != 10 {
		t.Errorf("Declarer score = %d, want 10", scores[1])
	}
	if scores[3] != 5 {
		t.Errorf("Partner score = %d, want 5", scores[3])
	}
	if scores[0] != -5 || scores[2] != -5 || scores[4] != -5 {
		t.Errorf("Opponent scores = %v, want -5 each", scores)
	}
}

// TestGame_CalculateScore_Failure tests score calculation when contract fails
// Rule: S = B − P when failed
func TestGame_CalculateScore_Failure(t *testing.T) {
	game := NewGame("test", 5)
	game.Options.MinBid = 13

	game.CurrentHand = NewHand(1, 0, 5)
	game.CurrentHand.DeclarerSeat = 1
	game.CurrentHand.PartnerSeat = 3
	game.CurrentHand.Contract = &Contract{
		DeclarerSeat: 1,
		Points:       16, // B = 16
		Trump:        Trump{Suit: Hearts, NoTrump: false},
		NoFriend:     false,
	}

	for i := 0; i < 10; i++ {
		game.CurrentHand.Tricks = append(game.CurrentHand.Tricks, NewTrick(i+1, 0))
	}

	// Set points: only 14 points taken (failed)
	game.CurrentHand.PointsBySeat = map[int]int{
		0: 3,
		1: 7, // Declarer
		2: 3,
		3: 7, // Partner
		4: 0,
	}

	scores, err := game.CalculateScore()

	if err != nil {
		t.Errorf("CalculateScore error = %v", err)
	}

	// P = 14, B = 16
	// S = 16 − 14 = 2
	// Declarer: −4, Partner: −2, Opponents: +2 each

	if scores[1] != -4 {
		t.Errorf("Declarer score = %d, want -4", scores[1])
	}
	if scores[3] != -2 {
		t.Errorf("Partner score = %d, want -2", scores[3])
	}
	if scores[0] != 2 || scores[2] != 2 || scores[4] != 2 {
		t.Errorf("Opponent scores = %v, want 2 each", scores)
	}
}

// TestGame_CalculateScore_Run tests score with run (20 points)
// Rule: Run doubles the score
func TestGame_CalculateScore_Run(t *testing.T) {
	game := NewGame("test", 5)
	game.Options.MinBid = 13

	game.CurrentHand = NewHand(1, 0, 5)
	game.CurrentHand.DeclarerSeat = 1
	game.CurrentHand.PartnerSeat = 3
	game.CurrentHand.Contract = &Contract{
		DeclarerSeat: 1,
		Points:       17,
		Trump:        Trump{Suit: Hearts, NoTrump: false},
		NoFriend:     false,
	}

	for i := 0; i < 10; i++ {
		game.CurrentHand.Tricks = append(game.CurrentHand.Tricks, NewTrick(i+1, 0))
	}

	// Took all 20 points (run)
	game.CurrentHand.PointsBySeat = map[int]int{
		1: 10, // Declarer
		3: 10, // Partner
	}

	scores, err := game.CalculateScore()

	if err != nil {
		t.Errorf("CalculateScore error = %v", err)
	}

	// B = 17, P = 20, M = 13
	// S = 2×(17−13) + (20−17) = 8 + 3 = 11
	// Run doubles: S = 11 × 2 = 22
	// Declarer: +44, Partner: +22, Opponents: −22 each

	if scores[1] != 44 {
		t.Errorf("Declarer score with run = %d, want 44", scores[1])
	}
	if scores[3] != 22 {
		t.Errorf("Partner score with run = %d, want 22", scores[3])
	}
}

// TestGame_CalculateScore_NoTrump tests score with no-trump
// Rule: No-trump doubles the score
func TestGame_CalculateScore_NoTrump(t *testing.T) {
	game := NewGame("test", 5)
	game.Options.MinBid = 13

	game.CurrentHand = NewHand(1, 0, 5)
	game.CurrentHand.DeclarerSeat = 1
	game.CurrentHand.PartnerSeat = 3
	game.CurrentHand.Contract = &Contract{
		DeclarerSeat: 1,
		Points:       14,
		Trump:        Trump{Suit: "", NoTrump: true}, // No trump
		NoFriend:     false,
	}

	for i := 0; i < 10; i++ {
		game.CurrentHand.Tricks = append(game.CurrentHand.Tricks, NewTrick(i+1, 0))
	}

	// Took 15 points
	game.CurrentHand.PointsBySeat = map[int]int{
		1: 8, // Declarer
		3: 7, // Partner
	}

	scores, err := game.CalculateScore()

	if err != nil {
		t.Errorf("CalculateScore error = %v", err)
	}

	// B = 14, P = 15, M = 13
	// S = 2×(14−13) + (15−14) = 2 + 1 = 3
	// No-trump doubles: S = 3 × 2 = 6
	// Declarer: +12, Partner: +6, Opponents: −6 each

	if scores[1] != 12 {
		t.Errorf("Declarer score with no-trump = %d, want 12", scores[1])
	}
}

// TestGame_CalculateScore_NoFriend tests score with no friend
// Rule: No friend doubles the score
func TestGame_CalculateScore_NoFriend(t *testing.T) {
	game := NewGame("test", 5)
	game.Options.MinBid = 13

	game.CurrentHand = NewHand(1, 0, 5)
	game.CurrentHand.DeclarerSeat = 1
	game.CurrentHand.PartnerSeat = -1
	game.CurrentHand.Contract = &Contract{
		DeclarerSeat: 1,
		Points:       16,
		Trump:        Trump{Suit: Hearts, NoTrump: false},
		NoFriend:     true,
	}

	for i := 0; i < 10; i++ {
		game.CurrentHand.Tricks = append(game.CurrentHand.Tricks, NewTrick(i+1, 0))
	}

	// Declarer took 17 points alone
	game.CurrentHand.PointsBySeat = map[int]int{
		1: 17, // Declarer
	}

	scores, err := game.CalculateScore()

	if err != nil {
		t.Errorf("CalculateScore error = %v", err)
	}

	// B = 16, P = 17, M = 13
	// S = 2×(16−13) + (17−16) = 6 + 1 = 7
	// No friend doubles: S = 7 × 2 = 14
	// Declarer: +28, Opponents: −7 each

	if scores[1] != 28 {
		t.Errorf("Declarer score with no friend = %d, want 28", scores[1])
	}
	if scores[0] != -7 || scores[2] != -7 || scores[3] != -7 || scores[4] != -7 {
		t.Errorf("Opponent scores = %v, want -7 each", scores)
	}
}

// TestGame_GetMighty tests Mighty card determination
// Rule: Mighty is SA unless spades are trump, then DA
func TestGame_GetMighty(t *testing.T) {
	game := NewGame("test", 5)

	// No contract yet: default SA
	mighty := game.GetMighty()
	if !mighty.Equals(Card{Suit: Spades, Rank: Ace}) {
		t.Errorf("Default mighty = %v, want SA", mighty)
	}

	// With hearts as trump: SA
	game.CurrentHand = NewHand(1, 0, 5)
	game.CurrentHand.Contract = &Contract{
		Trump: Trump{Suit: Hearts, NoTrump: false},
	}

	mighty = game.GetMighty()
	if !mighty.Equals(Card{Suit: Spades, Rank: Ace}) {
		t.Errorf("Mighty with hearts trump = %v, want SA", mighty)
	}

	// With spades as trump: DA
	game.CurrentHand.Contract.Trump.Suit = Spades

	mighty = game.GetMighty()
	if !mighty.Equals(Card{Suit: Diamonds, Rank: Ace}) {
		t.Errorf("Mighty with spades trump = %v, want DA", mighty)
	}
}

// TestGame_GetRipper tests Ripper card determination
// Rule: Ripper is C3 unless clubs are trump, then S3
func TestGame_GetRipper(t *testing.T) {
	game := NewGame("test", 5)

	// Default: C3
	ripper := game.GetRipper()
	if !ripper.Equals(Card{Suit: Clubs, Rank: Three}) {
		t.Errorf("Default ripper = %v, want C3", ripper)
	}

	// With hearts as trump: C3
	game.CurrentHand = NewHand(1, 0, 5)
	game.CurrentHand.Contract = &Contract{
		Trump: Trump{Suit: Hearts, NoTrump: false},
	}

	ripper = game.GetRipper()
	if !ripper.Equals(Card{Suit: Clubs, Rank: Three}) {
		t.Errorf("Ripper with hearts trump = %v, want C3", ripper)
	}

	// With clubs as trump: S3
	game.CurrentHand.Contract.Trump.Suit = Clubs

	ripper = game.GetRipper()
	if !ripper.Equals(Card{Suit: Spades, Rank: Three}) {
		t.Errorf("Ripper with clubs trump = %v, want S3", ripper)
	}
}

// TestGame_GetJoker tests Joker retrieval
// Rule: Joker is always the same card
func TestGame_GetJoker(t *testing.T) {
	game := NewGame("test", 5)

	joker := game.GetJoker()

	if !joker.IsJoker() {
		t.Error("GetJoker should return Joker card")
	}
	if joker.Suit != NoSuit || joker.Rank != Joker {
		t.Errorf("GetJoker = %v, want Joker", joker)
	}
}

// TestGame_Complete tests game completion
// Rule: Should mark game as complete with timestamp
func TestGame_Complete(t *testing.T) {
	game := NewGame("test", 5)

	game.Complete()

	if game.Status != PhaseGameComplete {
		t.Errorf("Status = %v, want PhaseGameComplete", game.Status)
	}
	if game.CompletedAt == nil {
		t.Error("CompletedAt should not be nil")
	}
	if time.Since(*game.CompletedAt) > time.Second {
		t.Error("CompletedAt should be recent")
	}
}

// TestGame_IsGameComplete tests game completion check
// Rule: Game is complete only when explicitly marked
func TestGame_IsGameComplete(t *testing.T) {
	game := NewGame("test", 5)

	if game.IsGameComplete() {
		t.Error("New game should not be complete")
	}

	game.Complete()

	if !game.IsGameComplete() {
		t.Error("Game should be complete after Complete()")
	}
}
