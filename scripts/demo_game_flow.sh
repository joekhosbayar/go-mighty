#!/bin/bash

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "jq is required but not installed. Please install jq first."
    exit 1
fi

BASE_URL="http://localhost:8080"
USERS=("alice" "bob" "carol" "dave" "eve")
TOKENS=()
USER_IDS=()
LAST_INDEX=$((${#USERS[@]} - 1))

echo "--- MIGHTY DEMO SCRIPT ---"
echo "0. Registering and authenticating 5 players..."

for i in "${!USERS[@]}"; do
  USERNAME="${USERS[$i]}_$(date +%s)"
  PASSWORD="password123"
  EMAIL="${USERNAME}@example.com"
  
  # Register
  echo "Registering ${USERNAME}..."
  REG_RES=$(curl -sS --fail-with-body -X POST "${BASE_URL}/auth/signup" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"${USERNAME}\", \"password\":\"${PASSWORD}\", \"email\":\"${EMAIL}\"}")

  if ! echo "$REG_RES" | jq . >/dev/null 2>&1; then
    echo "Signup returned invalid JSON for ${USERNAME}:"
    echo "$REG_RES"
    exit 1
  fi

  if ! USER_ID=$(jq -er '.id' <<< "$REG_RES"); then
    echo "Signup failed to return a valid user id for ${USERNAME}:"
    echo "$REG_RES"
    exit 1
  fi
  USER_IDS+=("$USER_ID")

  # Login
  echo "Logging in ${USERNAME}..."
  LOGIN_RES=$(curl -sS --fail-with-body -X POST "${BASE_URL}/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"${USERNAME}\", \"password\":\"${PASSWORD}\"}")

  if ! echo "$LOGIN_RES" | jq . >/dev/null 2>&1; then
    echo "Login returned invalid JSON for ${USERNAME}:"
    echo "$LOGIN_RES"
    exit 1
  fi

  if ! TOKEN=$(jq -er '.token' <<< "$LOGIN_RES"); then
    echo "Login failed to return a valid token for ${USERNAME}:"
    echo "$LOGIN_RES"
    exit 1
  fi
  TOKENS+=("$TOKEN")
done

echo -e "\n1. Player 0 (Alice) Creating Game..."
CREATE_RES=$(curl -sS --fail-with-body -X POST "${BASE_URL}/games" \
  -H "Authorization: Bearer ${TOKENS[0]}")

GAME_ID=$(jq -er '.id' <<< "$CREATE_RES")
echo "Created Game: ${GAME_ID}"
echo "$CREATE_RES" | jq .
echo -e "\n"

echo "2. Joining 4 other players (using Bearer Tokens)..."
for i in $(seq 1 $LAST_INDEX); do
  TOKEN="${TOKENS[$i]}"
  echo "Player ${i} joining..."
  
  JOIN_RES=$(curl -sS --fail-with-body -X POST "${BASE_URL}/games/${GAME_ID}/join" \
    -H "Authorization: Bearer ${TOKEN}")

  GAME_STATE="$JOIN_RES"
  if [ "$i" -eq "$LAST_INDEX" ]; then
    echo "Joining player $((LAST_INDEX + 1)) (Triggering Deal)..."
    echo "$GAME_STATE" | jq .status
  else
    echo "$JOIN_RES" | jq ".players[$i]"
  fi
done

# Extract current version
VERSION=$(jq -er '.version' <<< "$GAME_STATE")

echo -e "\nGame is now in 'bidding' phase."

# 3. Valid Bid via REST (If MoveHandler is authenticated, pass Bearer token)
echo "3. Player 1 (Alice) Bids 7 Spades via REST /move..."
# Note: Currently, /move might still require player_id if not fully refactored, but we'll include the token just in case
RESPONSE=$(curl -s -X POST "${BASE_URL}/games/${GAME_ID}/move" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKENS[0]}" \
  -d "{
    \"player_id\": \"${USER_IDS[0]}\",
    \"move_type\": \"bid\",
    \"client_version\": ${VERSION},
    \"payload\": {\"suit\":\"spades\", \"points\":7}
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
cat <<EOF
{
  "type": "MOVE",
  "move_type": "bid",
  "client_version": 2,
  "payload": {
    "suit": "hearts",
    "points": 8
  }
}
EOF

echo -e "\nDemo complete up to first bid."
