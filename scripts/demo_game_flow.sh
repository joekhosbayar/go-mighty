#!/bin/bash

BASE_URL="http://localhost:8080"
GAME_ID="game_demo_$(date +%s)"

echo "1. Creating Game ${GAME_ID}..."
curl -s -X POST "${BASE_URL}/games" \
  -H "Content-Type: application/json" \
  -d "{\"id\": \"${GAME_ID}\"}" | jq .
echo -e "\n"

# Players
P1="p1"
P2="p2"
P3="p3"
P4="p4"
P5="p5"

echo "2. Joining 5 players..."
curl -s -X POST "${BASE_URL}/games/${GAME_ID}/join" -d "{\"player_id\":\"${P1}\", \"name\":\"Alice\", \"seat\":0}" | jq .players[0]
curl -s -X POST "${BASE_URL}/games/${GAME_ID}/join" -d "{\"player_id\":\"${P2}\", \"name\":\"Bob\",   \"seat\":1}" | jq .players[1]
curl -s -X POST "${BASE_URL}/games/${GAME_ID}/join" -d "{\"player_id\":\"${P3}\", \"name\":\"Carol\", \"seat\":2}" | jq .players[2]
curl -s -X POST "${BASE_URL}/games/${GAME_ID}/join" -d "{\"player_id\":\"${P4}\", \"name\":\"Dave\",  \"seat\":3}" | jq .players[3]
# The 5th player triggers the game start and deal
echo "Joining 5th player (Triggering Deal)..."
GAME_STATE=$(curl -s -X POST "${BASE_URL}/games/${GAME_ID}/join" -d "{\"player_id\":\"${P5}\", \"name\":\"Eve\",   \"seat\":4}")
echo $GAME_STATE | jq .status

# Extract current version
VERSION=$(echo $GAME_STATE | jq .version)

echo -e "\nGame is now in 'bidding' phase."

# 3. Valid Bid
echo "3. Player 1 (Alice) Bids 13 Spades..."
RESPONSE=$(curl -s -X POST "${BASE_URL}/games/${GAME_ID}/move" \
  -d "{
    \"player_id\": \"${P1}\",
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

echo -e "\nDemo complete up to first bid."
echo "To continue, other players must pass or bid higher."
