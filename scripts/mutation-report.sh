#!/usr/bin/env bash
# Summarizes a gremlins JSON report (gremlins unleash -o <file>) as Markdown
# and enforces an efficacy threshold.
#
#   scripts/mutation-report.sh <report.json> [min-efficacy-percent] [min-judged]
#
# Efficacy = killed / (killed + lived): of the mutants a test executes, how
# many make a test fail. A LIVED mutant is a logic change the suite does not
# notice. The threshold only applies once at least min-judged mutants
# (killed + lived, default 1) were judged: on a tiny diff one equivalent
# mutant would otherwise decide the result, and a docs-only diff has none. The summary
# is appended to $GITHUB_STEP_SUMMARY when set (GitHub Actions).
set -euo pipefail

report="${1:?usage: mutation-report.sh <report.json> [min-efficacy]}"
threshold="${2:-0}"
min_judged="${3:-1}"

summary="$(jq -r '
  "## Mutation testing\n",
  "| Killed | Lived | Not covered | Timed out | Efficacy | Mutant coverage |",
  "|---:|---:|---:|---:|---:|---:|",
  "| \(.mutants_killed) | \(.mutants_lived) | \(.mutants_not_covered) | \([.files[].mutations[] | select(.status == "TIMED OUT")] | length) | \(if .mutants_killed + .mutants_lived > 0 then "\(.test_efficacy * 100 | round / 100)%" else "n/a" end) | \(.mutations_coverage * 100 | round / 100)% |",
  "",
  ( [ .files[] | .file_name as $f | .mutations[] | select(.status == "LIVED") | . + {file: $f} ]
    | sort_by(.file, .line, .column) as $lived
    | if ($lived | length) > 0 then
        "### Lived mutants\n",
        "Each row is a change to the code that no test noticed. Add a test that pins the behavior, or note why the mutant is equivalent (changes nothing observable).\n",
        "| Location | Mutation |", "|---|---|",
        ($lived[] | "| `\(.file):\(.line):\(.column)` | \(.type) |")
      else empty end )
' "$report")"

echo "$summary"
if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  echo "$summary" >>"$GITHUB_STEP_SUMMARY"
fi

judged="$(jq '.mutants_killed + .mutants_lived' "$report")"
efficacy="$(jq '.test_efficacy' "$report")"
if (( judged < min_judged )); then
  echo "Only ${judged} judged mutant(s), fewer than ${min_judged}: threshold not applied."
  exit 0
fi
if awk -v e="$efficacy" -v t="$threshold" 'BEGIN { exit !(e < t) }'; then
  echo "Mutation efficacy ${efficacy}% is below the ${threshold}% threshold." >&2
  exit 1
fi
