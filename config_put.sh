#!/bin/sh

FILE="gateway.json"
URL="http://k8s-0.den.vixen.lgbt:32054/actions/config"
NUM_SHARDS="32"

CONTENT=$(cat "$FILE")

read -r -d '' JSON_PAYLOAD <<EOF
{
  "num_shards": $NUM_SHARDS,
  "pod_definition": $CONTENT
}
EOF
response=$(curl -s -X POST "$URL" \
  -H "Content-Type: application/json" \
  -d "$JSON_PAYLOAD")
