#!/bin/sh

FILE="gateway.yaml"
URL="http://manager.pk.den.vixen.lgbt/actions/config"
NUM_SHARDS="32"

CONTENT=$(yq -o=json "$FILE")

read -r -d '' JSON_PAYLOAD <<EOF
{
  "NumShards": $NUM_SHARDS,
  "PodDefinition": $CONTENT
}
EOF
response=$(curl -s -X POST "$URL" \
  -H "Content-Type: application/json" \
  -d "$JSON_PAYLOAD")
