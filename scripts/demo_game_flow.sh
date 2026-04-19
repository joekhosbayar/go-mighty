#!/bin/bash

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "jq is required but not installed. Please install jq first."
    exit 1
fi

BASE_URL="http://localhost:8080"
GAME_ID="game_demo_$(date +%s)"
USERS=("alice" "bob" "carol" "dave" "eve")
SEATS=(0 1 2 3 4)
TOKENS=()
USER_IDS=()

echo "--- MIGHTY DEMO SCRIPT ---"
echo "0. Registering and authenticating 5 players..."

for i in "${!USERS[@]}"; do
  USERNAME="${USERS[$i]}_$(date +%s)"
  PASSWORD="password123"
  EMAIL="${USERNAME}@example.com"
  
  # Register
  echo "Registering ${USERNAME}..."
  REG_RES=$(curl -s -X POST "${BASE_URL}/auth/signup" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"${USERNAME}\", \"password\":\"${PASSWORD}\", \"email\":\"${EMAIL}\"}")
  
  USER_ID=$(echo $REG_RES | jq -r '.id')
  USER_IDS+=("$USER_ID")

  # Login
  echo "Logging in ${USERNAME}..."
  LOGIN_RES=$(curl -s -X POST "${BASE_URL}/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"${USERNAME}\", \"password\":\"${PASSWORD}\"}")
  
  TOKEN=$(echo $LOGIN_RES | jq -r '.token')
  TOKENS+=("$TOKEN")
done

echo -e "\n1. Creating Game ${GAME_ID}..."
curl -s -X POST "${BASE_URL}/games" \
  -H "Content-Type: application/json" \
  -d "{\"id\": \"${GAME_ID}\"}" | jq .
echo -e "\n"

echo "2. Joining 5 players (using Bearer Tokens)..."
for i in "${!USERS[@]}"; do
  SEAT="${SEATS[$i]}"
  TOKEN="${TOKENS[$i]}"
  echo "Player ${i} joining at seat ${SEAT}..."
  
  JOIN_RES=$(curl -s -X POST "${BASE_URL}/games/${GAME_ID}/join" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${TOKEN}" \
    -d "{\"seat\":${SEAT}}")
  
  if [ $i -eq 4 ]; then
    # The 5th player triggers the game start and deal
    echo "Joining 5th player (Triggering Deal)..."
    GAME_STATE=$JOIN_RES
    echo $GAME_STATE | jq .status
  else
    echo $JOIN_RES | jq ".players[$SEAT]"
  fi
done

# Extract current version
VERSION=$(echo $GAME_STATE | jq .version)

echo -e "\nGame is now in 'bidding' phase."

# 3. Valid Bid via REST (If MoveHandler is authenticated, pass Bearer token)
echo "3. Player 1 (Alice) Bids 13 Spades via REST /move..."
# Note: Currently, /move might still require player_id if not fully refactored, but we'll include the token just in case
RESPONSE=$(curl -s -X POST "${BASE_URL}/games/${GAME_ID}/move" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKENS[0]}" \
  -d "{
    \"player_id\": \"${USER_IDS[0]}\",
    \"move_type\": \"bid\",
    \"client_version\": ${VERSION},
    \"payload\": {\"suit\":\"spades\", \"points\":13}
  }")

if echo "$RESPONSE" | jq . >/dev/null 2>&1; then
    echo "$RESPONSE" | jq '.current_bid'
else
    echo "Error: Invalid JSON response"
    echo "$RESPONSE"
fi

echo -e "\n4. Real-time WebSocket details:"
echo "To connect to the WebSocket and submit moves, use a client (e.g. wscat):"
echo "  wscat -c \"ws://localhost:8080/games/${GAME_ID}/ws?token=${TOKENS[0]}\""
echo ""
echo "Example payload to send over WebSocket:"
echo '{
  "type": "MOVE",
  "move_type": "bid",
  "client_version": 2,
  "payload": {
    "suit": "hearts",
    "points": 14
  }
}'

echo -e "\nDemo complete up to first bid."
