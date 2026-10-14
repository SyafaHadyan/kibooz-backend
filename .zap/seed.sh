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

post_as() {
  curl -fsS -X POST "$base$1" -H "Authorization: Bearer $2" -H 'Content-Type: application/json' -d "$3"
}

# pick reads a value from a response and stops the script when it is missing, so a changed response fails loudly
pick() {
  local value
  value="$(jq -r "$2" <<<"$1")"

  if [ -z "$value" ] || [ "$value" = null ]; then
    echo "seed.sh could not read $2 from $1" >&2
    exit 1
  fi

  echo "$value"
}

guru="$(post /auth/register "$(jq -n --arg e "zap.teacher.$suffix@example.com" --arg p "$pass" \
  '{email: $e, password: $p, fullName: "ZAP Teacher", role: "GURU", class: {name: "zap", gradeLevel: "Class Z"}}')")"
guru_session="$(jq -r '.data.token' <<<"$guru")"
dashboard="$(get /guru/dashboard "$guru_session")"
code="$(jq -r '.data.classOverview.joinCode' <<<"$dashboard")"

wali_email="zap.parent.$suffix@example.com"
wali="$(post /auth/register "$(jq -n --arg e "$wali_email" --arg p "$pass" --arg c "$code" \
  '{email: $e, password: $p, fullName: "ZAP Parent", role: "WALI", classCode: $c, student: {nisn: "123456789012", fullName: "ZAP Child"}}')")"
wali_session="$(jq -r '.data.token' <<<"$wali")"
student="$(get /wali/dashboard "$wali_session" | jq -r '.data.student.id')"

requests="$(jq -n --arg gt "$guru_session" --arg wt "$wali_session" --arg s "$student" --arg e "$wali_email" --arg p "$pass" --arg w "$pass-wrong" '[
  {method: "GET", path: "/healthz"},
  {method: "GET", path: "/api/v1/guru/dashboard", token: $gt},
  {method: "GET", path: "/api/v1/guru/mood/analytics?range=monthly", token: $gt},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, body: {studentId: $s, moodType: "SENANG"}},
  {method: "GET", path: "/api/v1/wali/dashboard", token: $wt},
  {method: "POST", path: "/api/v1/trash/scan-claim", token: $wt, body: {studentId: $s, trashType: "ORGANIK", confidenceScore: 0.9}},
  {method: "GET", path: "/api/v1/leaderboard", token: $wt},
  {method: "POST", path: "/api/v1/auth/login", body: {email: $e, password: $p, role: "WALI"}},
  {method: "POST", path: "/api/v1/auth/login", body: {email: $e, password: $w, role: "WALI"}},
  {method: "GET", path: "/api/v1/guru/dashboard"},
  {method: "GET", path: "/api/v1/guru/dashboard", token: $wt},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, body: {}},
  {method: "GET", path: "/api/v1/does-not-exist"}
]')"

# The passive baseline scan fails on any warning, so it only gets the list above. The active scan sets ZAP_FULL=true to attack
# the rest of the API too, which is where registration, sessions, the class pages, the forum and photos live.
if [ "${ZAP_FULL:-false}" != true ]; then
  echo "$requests" >"$out"
  exit 0
fi

guru_refresh="$(pick "$guru" '.data.refreshToken')"
wali_refresh="$(pick "$wali" '.data.refreshToken')"
class="$(pick "$dashboard" '.data.classOverview.classId')"

video="$(post_as "/classes/$class/videos" "$guru_session" '{"title": "ZAP video", "videoUrl": "https://example.com/zap.mp4"}')"
video_id="$(pick "$video" '.data.id')"
thread="$(post_as "/classes/$class/forum" "$guru_session" '{"title": "ZAP thread", "body": "ZAP thread body"}')"
thread_id="$(pick "$thread" '.data.id')"
reply="$(post_as "/classes/$class/forum/$thread_id/replies" "$guru_session" '{"body": "ZAP reply"}')"
reply_id="$(pick "$reply" '.data.id')"

# a second class, so that replacing its join code never takes the code that the parent registration above relies on
other="$(post_as /guru/classes "$guru_session" '{"name": "zap two", "gradeLevel": "Class Y"}')"
other_id="$(pick "$other" '.data.id')"

# a 1x1 PNG, so that the image checks run on a real picture and not only on a refusal
photo="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

extra="$(jq -n --arg gt "$guru_session" --arg wt "$wali_session" --arg s "$student" --arg c "$class" --arg v "$video_id" \
  --arg t "$thread_id" --arg r "$reply_id" --arg o "$other_id" --arg code "$code" --arg gr "$guru_refresh" --arg wr "$wali_refresh" \
  --arg p "$pass" --arg w "$pass-wrong" --arg x "$suffix" --arg photo "$photo" '[
  {method: "POST", path: "/api/v1/auth/register", body: {email: ("zap.second.teacher." + $x + "@example.com"), password: $p, fullName: "ZAP Second Teacher", role: "GURU", class: {name: "zap three"}}},
  {method: "POST", path: "/api/v1/auth/register", body: {email: ("zap.second.parent." + $x + "@example.com"), password: $p, fullName: "ZAP Second Parent", role: "WALI", classCode: $code, student: {nisn: "223456789012", fullName: "ZAP Second Child"}}},
  {method: "POST", path: "/api/v1/auth/register", body: {email: ("zap.nocode." + $x + "@example.com"), password: $p, fullName: "ZAP No Code", role: "WALI", classCode: "ZZZZZZZZ", student: {nisn: "323456789012", fullName: "ZAP Nobody"}}},
  {method: "POST", path: "/api/v1/auth/refresh-token", body: {refreshToken: $wr}},
  {method: "POST", path: "/api/v1/auth/logout", body: {refreshToken: $gr}},
  {method: "GET", path: ("/api/v1/classes/" + $c + "/videos"), token: $gt},
  {method: "GET", path: ("/api/v1/classes/" + $c + "/videos"), token: $wt},
  {method: "POST", path: ("/api/v1/classes/" + $c + "/videos"), token: $gt, body: {title: "ZAP video two", videoUrl: "https://example.com/zap-two.mp4", durationSeconds: 5}},
  {method: "POST", path: ("/api/v1/classes/" + $c + "/videos"), token: $wt, body: {title: "parent video", videoUrl: "https://example.com/p.mp4"}},
  {method: "POST", path: ("/api/v1/classes/" + $c + "/videos/upload-url"), token: $gt, body: {contentType: "video/mp4", sizeBytes: 1024}},
  {method: "PATCH", path: ("/api/v1/classes/" + $c + "/videos/" + $v), token: $gt, body: {title: "ZAP video renamed"}},
  {method: "GET", path: ("/api/v1/classes/" + $c + "/students"), token: $gt},
  {method: "GET", path: ("/api/v1/classes/" + $c + "/students"), token: $wt},
  {method: "GET", path: ("/api/v1/classes/" + $c + "/forum"), token: $wt},
  {method: "POST", path: ("/api/v1/classes/" + $c + "/forum"), token: $wt, body: {title: "ZAP parent thread", body: "ZAP parent body"}},
  {method: "PATCH", path: ("/api/v1/classes/" + $c + "/forum/" + $t), token: $gt, body: {body: "ZAP edited thread"}},
  {method: "PATCH", path: ("/api/v1/classes/" + $c + "/forum/" + $t), token: $wt, body: {body: "not the author"}},
  {method: "GET", path: ("/api/v1/classes/" + $c + "/forum/" + $t + "/replies"), token: $gt},
  {method: "POST", path: ("/api/v1/classes/" + $c + "/forum/" + $t + "/replies"), token: $wt, body: {body: "ZAP parent reply"}},
  {method: "PATCH", path: ("/api/v1/classes/" + $c + "/forum/" + $t + "/replies/" + $r), token: $gt, body: {body: "ZAP edited reply"}},
  {method: "GET", path: "/api/v1/guru/classes", token: $gt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $c), token: $gt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $c), token: $wt},
  {method: "POST", path: "/api/v1/guru/classes", token: $gt, body: {name: "zap four"}},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/join-code"), token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/join-code"), token: $wt},
  {method: "GET", path: "/api/v1/guru/profile", token: $gt},
  {method: "GET", path: "/api/v1/guru/profile/detail", token: $gt},
  {method: "PUT", path: "/api/v1/guru/profile", token: $gt, body: {phoneNumber: "+62 812-0000-0000", address: "ZAP street 1"}},
  {method: "GET", path: ("/api/v1/wali/child/" + $s), token: $wt},
  {method: "POST", path: "/api/v1/wali/guidance/apply", token: $wt, body: {guidanceId: "guidance-senang-4step", studentId: $s, parentNotes: "ZAP notes"}},
  {method: "GET", path: "/api/v1/trash/stats", token: $wt},
  {method: "POST", path: "/api/v1/trash/scan-claim", token: $wt, body: {studentId: $s, trashType: "ANORGANIK", confidenceScore: 0.9, photoBase64: $photo}},
  {method: "DELETE", path: "/api/v1/users/me", token: $wt, body: {password: $w}}
]')"

# the account deletion above uses a wrong password on purpose, so the scan attacks the check without removing the account it needs
jq -n --argjson a "$requests" --argjson b "$extra" '$a + $b' >"$out"
