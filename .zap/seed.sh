#!/bin/bash
# Creates a teacher, a parent and a child on the stack under test and writes the requests that ZAP replays.
# The JSON API has no links for the ZAP spider to follow, so this is how ZAP gets to see real responses.
set -euo pipefail

base="${BASE_URL:-http://127.0.0.1:8080}/api/v1"
out="${1:?usage seed.sh <output file>}"
pass="zap$(openssl rand -hex 12)"
suffix="$(openssl rand -hex 6)"

post() {
  curl -fsS -X POST "$base$1" -H 'Content-Type: application/json' -d "$2"
}

get() {
  curl -fsS "$base$1" -H "Authorization: Bearer $2"
}

guru="$(post /auth/register "$(jq -n --arg e "zap.teacher.$suffix@example.com" --arg p "$pass" \
  '{email: $e, password: $p, fullName: "ZAP Teacher", role: "GURU", class: {name: "zap", gradeLevel: "Class Z"}}')")"
guru_token="$(jq -r '.data.token' <<<"$guru")"
code="$(get /guru/dashboard "$guru_token" | jq -r '.data.classOverview.joinCode')"

wali_email="zap.parent.$suffix@example.com"
wali="$(post /auth/register "$(jq -n --arg e "$wali_email" --arg p "$pass" --arg c "$code" \
  '{email: $e, password: $p, fullName: "ZAP Parent", role: "WALI", classCode: $c, student: {nisn: "123456789012", fullName: "ZAP Child"}}')")"
wali_token="$(jq -r '.data.token' <<<"$wali")"
student="$(get /wali/dashboard "$wali_token" | jq -r '.data.student.id')"

jq -n --arg gt "$guru_token" --arg wt "$wali_token" --arg s "$student" --arg e "$wali_email" --arg p "$pass" '[
  {method: "GET", path: "/healthz"},
  {method: "GET", path: "/api/v1/guru/dashboard", token: $gt},
  {method: "GET", path: "/api/v1/guru/mood/analytics?range=monthly", token: $gt},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, body: {studentId: $s, moodType: "SENANG"}},
  {method: "GET", path: "/api/v1/wali/dashboard", token: $wt},
  {method: "POST", path: "/api/v1/trash/scan-claim", token: $wt, body: {studentId: $s, trashType: "ORGANIK", confidenceScore: 0.9}},
  {method: "GET", path: "/api/v1/leaderboard", token: $wt},
  {method: "POST", path: "/api/v1/auth/login", body: {email: $e, password: $p, role: "WALI"}},
  {method: "POST", path: "/api/v1/auth/login", body: {email: $e, password: "wrong-password", role: "WALI"}},
  {method: "GET", path: "/api/v1/guru/dashboard"},
  {method: "GET", path: "/api/v1/guru/dashboard", token: $wt},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, body: {}},
  {method: "GET", path: "/api/v1/does-not-exist"}
]' >"$out"
