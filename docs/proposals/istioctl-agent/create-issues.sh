#!/bin/bash
# Script to create GitHub issues from proposal markdown files
# Run this from the repository root with: ./docs/proposals/istioctl-agent/create-issues.sh
#
# Prerequisites:
#   - gh CLI installed and authenticated with write access
#   - Run from the root of the istio repository
#
# This will create:
#   1. A parent epic issue
#   2. Eight sub-issues linked to the parent

set -euo pipefail

REPO="keithmattix/istio"
PROPOSAL_DIR="docs/proposals/istioctl-agent"

echo "Creating issues in $REPO..."

# Create parent epic
echo "Creating epic issue..."
EPIC_URL=$(gh issue create \
  --repo "$REPO" \
  --title "[Epic] istioctl AI Troubleshooting Agent" \
  --body-file "$PROPOSAL_DIR/00-epic.md" \
  --label "enhancement")
EPIC_NUM=$(echo "$EPIC_URL" | grep -oP '\d+$')
echo "Created epic: $EPIC_URL (issue #$EPIC_NUM)"

# Create sub-issues
declare -A ISSUES=(
  ["01-core-agent-framework.md"]="[Agent] Core Agent Framework & CLI Integration"
  ["02-diagnostic-tools.md"]="[Agent] Diagnostic Tool Wrappers"
  ["03-guided-operations.md"]="[Agent] Guided Operations Framework"
  ["04-boundary-detection.md"]="[Agent] Boundary Detection & Cross-Component Awareness"
  ["05-ambient-mode-tools.md"]="[Agent] Ambient Mode Specialized Tools (ztunnel, waypoint management)"
  ["06-sidecar-mode-tools.md"]="[Agent] Sidecar-Only Features (Sidecar CRD, exportTo, injection)"
  ["07-envoy-deep-inspection.md"]="[Agent] Envoy Proxy Inspection & Deep Analysis (all proxy types)"
  ["08-testing-docs.md"]="[Agent] Testing, Documentation & UX"
)

for file in $(echo "${!ISSUES[@]}" | tr ' ' '\n' | sort); do
  title="${ISSUES[$file]}"
  echo "Creating: $title"

  # Prepend parent reference to body
  BODY="Parent: $EPIC_URL\n\n$(cat "$PROPOSAL_DIR/$file")"

  SUB_URL=$(echo -e "$BODY" | gh issue create \
    --repo "$REPO" \
    --title "$title" \
    --body-file - \
    --label "enhancement")
  echo "  Created: $SUB_URL"
done

echo ""
echo "All issues created! Epic: $EPIC_URL"
echo "You can now link sub-issues to the epic via the GitHub UI or:"
echo "  gh issue edit $EPIC_NUM --repo $REPO --add-sub-issue <sub-issue-number>"
