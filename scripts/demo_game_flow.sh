#!/bin/bash

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "jq is required but not installed. Please install jq first."
    exit 1
fi

BASE_URL="${BASE_URL:-http://localhost:8080}"
USERS=("alice" "bob" "carol" "dave" "eve")
TOKENS=()
USER_IDS=()
LAST_INDEX=$((${#USERS[@]} - 1))

POOL_ID="${POOL_ID:?set POOL_ID (tofu output -raw cognito_pool_id)}"
CLIENT_ID="${CLIENT_ID:?set CLIENT_ID (tofu output -raw cognito_client_id)}"

echo "--- MIGHTY DEMO SCRIPT ---"
echo "0. Registering and authenticating 5 players..."

for i in "${!USERS[@]}"; do
  USERNAME="${USERS[$i]}_$(date +%s)"
  PASSWORD="MightyDemo1"
  EMAIL="${USERNAME}@example.com"

  echo "Creating Cognito user ${USERNAME}..."
  aws cognito-idp admin-create-user --region us-east-1 --user-pool-id "$POOL_ID" \
    --username "$EMAIL" --message-action SUPPRESS \
    --user-attributes Name=email,Value="$EMAIL" Name=email_verified,Value=true Name=preferred_username,Value="$USERNAME" >/dev/null
  aws cognito-idp admin-set-user-password --region us-east-1 --user-pool-id "$POOL_ID" \
    --username "$EMAIL" --password "$PASSWORD" --permanent

  TOKEN=$(aws cognito-idp admin-initiate-auth --region us-east-1 --user-pool-id "$POOL_ID" \
    --client-id "$CLIENT_ID" --auth-flow ADMIN_USER_PASSWORD_AUTH \
    --auth-parameters USERNAME="$EMAIL",PASSWORD="$PASSWORD" \
    --query 'AuthenticationResult.AccessToken' --output text)
  TOKENS+=("$TOKEN")

  USER_ID=$(aws cognito-idp admin-get-user --region us-east-1 --user-pool-id "$POOL_ID" \
    --username "$EMAIL" --query "UserAttributes[?Name=='sub'].Value" --output text)
  USER_IDS+=("$USER_ID")
done

echo -e "\n1. Player 0 (Alice) Creating Game..."
CREATE_RES=$(curl -sS --fail-with-body -X POST "${BASE_URL}/games" \
  -H "Authorization: Bearer ${TOKENS[0]}")

if ! GAME_ID=$(jq -er '.id' <<< "$CREATE_RES"); then
  echo "Error: Failed to extract game id from create-game response"
  echo "$CREATE_RES"
  exit 1
fi
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
echo "  wscat -c \"ws://localhost:8080/games/${GAME_ID}/ws\""
echo ""
echo "First, authenticate your connection:"
cat <<EOF
{
  "type": "AUTH",
  "token": "${TOKENS[0]}"
}
EOF
echo ""
echo "Example payload to send over WebSocket:"
cat <<EOF
{
  "type": "MOVE",
  "move_type": "bid",
  "client_version": ${VERSION},
  "payload": {
    "suit": "hearts",
    "points": 8
  }
}
EOF

echo -e "\nDemo complete up to first bid."
