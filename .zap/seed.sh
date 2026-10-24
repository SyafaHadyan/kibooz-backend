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

# a conversation between the teacher and the parent with one message in it, so that the chat routes answer real data
parent_user="$(pick "$(get /chat/contacts "$guru_session")" '.data.contacts[0].id')"
teacher_user="$(pick "$(get /chat/contacts "$wali_session")" '.data.contacts[0].id')"
conversation="$(post_as /chat/conversations "$guru_session" "$(jq -n --arg u "$parent_user" '{userId: $u}')")"
conversation_id="$(pick "$conversation" '.data.id')"
message="$(post_as "/chat/conversations/$conversation_id/messages" "$guru_session" '{"body": "ZAP chat message"}')"
message_id="$(pick "$message" '.data.id')"

# the room of the class, with one message in it
room="$(post_as /chat/conversations "$guru_session" "$(jq -n --arg c "$class" '{classId: $c}')")"
room_id="$(pick "$room" '.data.id')"
post_as "/chat/conversations/$room_id/messages" "$guru_session" '{"body": "ZAP room message"}' >/dev/null

# a second class, so that replacing its join code never takes the code that the parent registration above relies on
other="$(post_as /guru/classes "$guru_session" '{"name": "zap two", "gradeLevel": "Class Y"}')"
other_id="$(pick "$other" '.data.id')"

# a teacher whose password the scan really changes, so that no other request loses the login it relies on
pw_teacher="$(post /auth/register "$(jq -n --arg e "zap.password.$suffix@example.com" --arg p "$pass" \
  '{email: $e, password: $p, fullName: "ZAP Password", role: "GURU", class: {name: "zap password"}}')")"
pw_token="$(pick "$pw_teacher" '.data.token')"

# a teacher with a class that has no child who then deletes the account without a successor, which archives the class, and
# a parent who tries the join code of that class afterwards
leaver="$(post /auth/register "$(jq -n --arg e "zap.leaver.$suffix@example.com" --arg p "$pass" \
  '{email: $e, password: $p, fullName: "ZAP Leaver", role: "GURU", class: {name: "zap leaver"}}')")"
leaver_token="$(pick "$leaver" '.data.token')"
leaver_code="$(pick "$(get /guru/dashboard "$leaver_token")" '.data.classOverview.joinCode')"

# a teacher and a class of their own take the children that move, because a class with a child would not be archived when the leaver
# above deletes the account without a successor
target="$(post /auth/register "$(jq -n --arg e "zap.target.$suffix@example.com" --arg p "$pass" \
  '{email: $e, password: $p, fullName: "ZAP Target", role: "GURU", class: {name: "zap target"}}')")"
target_token="$(pick "$target" '.data.token')"
target_dashboard="$(get /guru/dashboard "$target_token")"
target_code="$(pick "$target_dashboard" '.data.classOverview.joinCode')"
target_class="$(pick "$target_dashboard" '.data.classOverview.classId')"

# move_child adds a child to the class of the first teacher and asks for it to move into the class of the target teacher. It sets moved_child
# and moved_request, so that the scan can accept one request, reject one and withdraw one, and read both lists. The values are set in
# the shell itself and not in a subshell, so a response that lacks an id stops the script.
move_child() {
  local child request
  child="$(post_as /wali/children "$wali_session" "$(jq -n --arg c "$code" --arg n "$1" --arg f "$2" \
    '{classCode: $c, student: {nisn: $n, fullName: $f}}')")"
  moved_child="$(pick "$child" '.data.id')"
  request="$(post_as "/wali/children/$moved_child/move-requests" "$wali_session" "$(jq -n --arg c "$target_code" '{classCode: $c}')")"
  moved_request="$(pick "$request" '.data.id')"
}

move_child 523456789012 'ZAP Mover One'
accept_child="$moved_child"
accept_request="$moved_request"
move_child 623456789012 'ZAP Mover Two'
reject_child="$moved_child"
reject_request="$moved_request"
move_child 723456789012 'ZAP Mover Three'
cancel_child="$moved_child"
cancel_request="$moved_request"

# times for the recordedAt of a mood log that was queued offline
past="$(date -u -d '-1 hour' +%Y-%m-%dT%H:%M:%SZ)"
future="$(date -u -d '+2 hours' +%Y-%m-%dT%H:%M:%SZ)"
ancient="$(date -u -d '-30 days' +%Y-%m-%dT%H:%M:%SZ)"

# a 1x1 PNG, so that the image checks run on a real picture and not only on a refusal
photo="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

# The stack under test has no bucket, so upload-url answers 503 and no pending file can exist. The two DELETE requests of
# /videos/upload-url below therefore only attack how the videoUrl query is read and refused, which is all that can run here.
extra="$(jq -n --arg gt "$guru_session" --arg wt "$wali_session" --arg s "$student" --arg c "$class" --arg v "$video_id" \
  --arg t "$thread_id" --arg r "$reply_id" --arg o "$other_id" --arg code "$code" --arg gr "$guru_refresh" --arg wr "$wali_refresh" \
  --arg p "$pass" --arg w "$pass-wrong" --arg x "$suffix" --arg photo "$photo" --arg pw "$pw_token" --arg lt "$leaver_token" \
  --arg lc "$leaver_code" --arg past "$past" --arg future "$future" --arg ancient "$ancient" --arg ci "$conversation_id" --arg ri "$room_id" \
  --arg mi "$message_id" --arg tu "$teacher_user" --arg lk "$target_class" --arg mt "$target_token" --arg mc "$target_code" --arg ac "$accept_child" --arg ar "$accept_request" \
  --arg rc "$reject_child" --arg rr "$reject_request" --arg cc "$cancel_child" --arg cr "$cancel_request" '[
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
  {method: "DELETE", path: ("/api/v1/classes/" + $c + "/videos/upload-url?videoUrl=https%3A%2F%2Fexample.com%2Fpending%2Fvideos%2F" + $c + "%2Fzap.mp4"), token: $gt},
  {method: "DELETE", path: ("/api/v1/classes/" + $c + "/videos/upload-url?videoUrl=https%3A%2F%2Fexample.com%2Fzap.mp4"), token: $wt},
  {method: "POST", path: "/api/v1/guru/mood/log/batch", token: $gt, body: {classId: $c, entries: [{studentId: $s, moodType: "SENANG", confidenceScore: 0.9}]}},
  {method: "POST", path: "/api/v1/guru/mood/log/batch", token: $gt, body: {classId: $c, entries: [{studentId: $s, moodType: "SEDIH", source: "MANUAL_INPUT"}]}},
  {method: "POST", path: "/api/v1/guru/mood/log/batch", token: $gt, body: {classId: $c, entries: [{studentId: $s, moodType: "SENANG", confidenceScore: 0.9}, {studentId: $s, moodType: "MARAH", confidenceScore: 0.9}]}},
  {method: "POST", path: "/api/v1/guru/mood/log/batch", token: $gt, body: {classId: $c, entries: []}},
  {method: "POST", path: "/api/v1/guru/mood/log/batch", token: $gt, body: {}},
  {method: "POST", path: "/api/v1/guru/mood/log/batch", token: $wt, body: {classId: $c, entries: [{studentId: $s, moodType: "SENANG", confidenceScore: 0.9}]}},
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
  {method: "GET", path: "/api/v1/chat/contacts", token: $gt},
  {method: "GET", path: "/api/v1/chat/contacts", token: $wt},
  {method: "GET", path: "/api/v1/chat/conversations", token: $gt},
  {method: "GET", path: "/api/v1/chat/conversations", token: $wt},
  {method: "POST", path: "/api/v1/chat/conversations", token: $wt, body: {userId: $tu}},
  {method: "POST", path: "/api/v1/chat/conversations", token: $gt, body: {userId: $tu}},
  {method: "POST", path: "/api/v1/chat/conversations", token: $gt, body: {userId: "not-a-uuid"}},
  {method: "POST", path: "/api/v1/chat/conversations", token: $gt, body: {}},
  {method: "POST", path: "/api/v1/chat/conversations", token: $wt, body: {classId: $c}},
  {method: "POST", path: "/api/v1/chat/conversations", token: $gt, body: {classId: "not-a-uuid"}},
  {method: "POST", path: "/api/v1/chat/conversations", token: $pw, body: {classId: $c}},
  {method: "GET", path: ("/api/v1/chat/conversations/" + $ri + "/messages"), token: $wt},
  {method: "GET", path: ("/api/v1/chat/conversations/" + $ri + "/messages"), token: $pw},
  {method: "POST", path: ("/api/v1/chat/conversations/" + $ri + "/messages"), token: $wt, body: {body: "ZAP room reply"}},
  {method: "POST", path: "/api/v1/push/tokens", token: $wt, body: {token: ("zap-token-" + $x + "-aaaaaaaaaaaaaaaa")}},
  {method: "POST", path: "/api/v1/push/tokens", token: $gt, body: {token: "short"}},
  {method: "DELETE", path: "/api/v1/push/tokens", token: $wt, body: {token: ("zap-token-" + $x + "-aaaaaaaaaaaaaaaa")}},
  {method: "POST", path: ("/api/v1/chat/conversations/" + $ci + "/media/video-upload"), token: $wt, body: {contentType: "video/mp4", sizeBytes: 1000}},
  {method: "POST", path: ("/api/v1/chat/conversations/" + $ci + "/media/video-upload"), token: $wt, body: {contentType: "text/html", sizeBytes: -1}},
  {method: "POST", path: ("/api/v1/chat/conversations/" + $ci + "/messages"), token: $wt, body: {mediaKey: "pending/chat/x/y/z.png"}},
  {method: "POST", path: ("/api/v1/chat/conversations/" + $ci + "/messages"), token: $wt, body: {mediaKey: "../../etc/passwd"}},
  {method: "GET", path: ("/api/v1/chat/conversations/" + $ci + "/messages"), token: $wt},
  {method: "GET", path: ("/api/v1/chat/conversations/" + $ci + "/messages?after=0&limit=10"), token: $gt},
  {method: "GET", path: ("/api/v1/chat/conversations/" + $ci + "/messages?before=" + $mi + "&limit=10"), token: $wt},
  {method: "GET", path: ("/api/v1/chat/conversations/" + $ci + "/messages?after=x&limit=0"), token: $gt},
  {method: "GET", path: ("/api/v1/chat/conversations/" + $ci + "/messages"), token: $pw},
  {method: "POST", path: ("/api/v1/chat/conversations/" + $ci + "/messages"), token: $wt, body: {body: "ZAP parent chat"}},
  {method: "POST", path: ("/api/v1/chat/conversations/" + $ci + "/messages"), token: $gt, key: ("zapkey-" + $x + "-3"), body: {body: "ZAP keyed chat"}},
  {method: "POST", path: ("/api/v1/chat/conversations/" + $ci + "/messages"), token: $gt, key: ("zapkey-" + $x + "-3"), body: {body: "ZAP keyed chat"}},
  {method: "POST", path: ("/api/v1/chat/conversations/" + $ci + "/messages"), token: $gt, body: {body: "   "}},
  {method: "POST", path: ("/api/v1/chat/conversations/" + $ci + "/messages"), token: $gt, body: {}},
  {method: "POST", path: ("/api/v1/chat/conversations/" + $ci + "/messages"), token: $pw, body: {body: "not a member"}},
  {method: "PUT", path: ("/api/v1/chat/conversations/" + $ci + "/read"), token: $wt, body: {messageId: 1000000}},
  {method: "PUT", path: ("/api/v1/chat/conversations/" + $ci + "/read"), token: $gt, body: {messageId: -1}},
  {method: "PUT", path: ("/api/v1/chat/conversations/" + $ci + "/read"), token: $pw, body: {messageId: 1}},
  {method: "DELETE", path: ("/api/v1/chat/conversations/" + $ci + "/messages/" + $mi), token: $wt},
  {method: "DELETE", path: ("/api/v1/chat/conversations/" + $ci + "/messages/" + $mi), token: $pw},
  {method: "DELETE", path: ("/api/v1/chat/conversations/" + $ci + "/messages/0"), token: $gt},
  {method: "DELETE", path: ("/api/v1/chat/conversations/" + $ci + "/messages/" + $mi), token: $gt},
  {method: "GET", path: "/api/v1/guru/classes", token: $gt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $c), token: $gt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $c), token: $wt},
  {method: "POST", path: "/api/v1/guru/classes", token: $gt, body: {name: "zap four"}},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/join-code"), token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/join-code"), token: $wt},
  {method: "PUT", path: ("/api/v1/guru/classes/" + $o + "/joining"), token: $gt, body: {joinOpen: false}},
  {method: "PUT", path: ("/api/v1/guru/classes/" + $o + "/joining"), token: $gt, body: {joinOpen: true}},
  {method: "PUT", path: ("/api/v1/guru/classes/" + $o + "/joining"), token: $gt, body: {}},
  {method: "PUT", path: ("/api/v1/guru/classes/" + $o + "/joining"), token: $wt, body: {joinOpen: false}},
  {method: "POST", path: ("/api/v1/guru/classes/" + $c + "/students/" + $s + "/nisn-verification"), token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $c + "/students/" + $s + "/nisn-verification"), token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $c + "/students/" + $s + "/nisn-verification"), token: $wt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $c + "/students/not-a-uuid/nisn-verification"), token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $c + "/students/00000000-0000-4000-8000-000000000000/nisn-verification"), token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/00000000-0000-4000-8000-000000000000/students/" + $s + "/nisn-verification"), token: $gt},
  {method: "DELETE", path: ("/api/v1/guru/classes/" + $c + "/students/" + $s + "/nisn-verification"), token: $wt},
  {method: "DELETE", path: ("/api/v1/guru/classes/" + $c + "/students/not-a-uuid/nisn-verification"), token: $gt},
  {method: "DELETE", path: ("/api/v1/guru/classes/" + $c + "/students/" + $s + "/nisn-verification"), token: $gt},
  {method: "DELETE", path: ("/api/v1/guru/classes/" + $c + "/students/" + $s), token: $wt},
  {method: "DELETE", path: ("/api/v1/guru/classes/" + $c + "/students/not-a-uuid"), token: $gt},
  {method: "DELETE", path: ("/api/v1/guru/classes/" + $c + "/students/00000000-0000-4000-8000-000000000000"), token: $gt},
  {method: "GET", path: ("/api/v1/wali/children/" + $ac + "/move-requests"), token: $wt},
  {method: "GET", path: ("/api/v1/wali/children/" + $ac + "/move-requests"), token: $gt},
  {method: "GET", path: "/api/v1/wali/children/not-a-uuid/move-requests", token: $wt},
  {method: "POST", path: ("/api/v1/wali/children/" + $s + "/move-requests"), token: $wt, body: {classCode: $code}},
  {method: "POST", path: ("/api/v1/wali/children/" + $s + "/move-requests"), token: $wt, body: {classCode: "ZZZZ9999"}},
  {method: "POST", path: ("/api/v1/wali/children/" + $s + "/move-requests"), token: $wt, body: {}},
  {method: "POST", path: ("/api/v1/wali/children/" + $s + "/move-requests"), token: $gt, body: {classCode: $mc}},
  {method: "DELETE", path: ("/api/v1/wali/children/" + $cc + "/move-requests/" + $cr), token: $gt},
  {method: "DELETE", path: ("/api/v1/wali/children/" + $cc + "/move-requests/" + $cr), token: $wt},
  {method: "DELETE", path: ("/api/v1/wali/children/" + $cc + "/move-requests/" + $cr), token: $wt},
  {method: "DELETE", path: ("/api/v1/wali/children/" + $cc + "/move-requests/not-a-uuid"), token: $wt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $lk + "/move-requests"), token: $mt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $lk + "/move-requests"), token: $gt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $lk + "/move-requests"), token: $wt},
  {method: "GET", path: "/api/v1/guru/classes/not-a-uuid/move-requests", token: $mt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $lk + "/move-requests/" + $rr + "/reject"), token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $lk + "/move-requests/" + $rr + "/reject"), token: $wt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $lk + "/move-requests/" + $rr + "/reject"), token: $mt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $lk + "/move-requests/" + $rr + "/reject"), token: $mt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $lk + "/move-requests/not-a-uuid/reject"), token: $mt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $lk + "/move-requests/" + $ar + "/accept"), token: $wt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $lk + "/move-requests/" + $ar + "/accept"), token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $lk + "/move-requests/" + $ar + "/accept"), token: $mt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $lk + "/move-requests/" + $ar + "/accept"), token: $mt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $lk + "/move-requests/00000000-0000-4000-8000-000000000000/accept"), token: $mt},
  {method: "GET", path: ("/api/v1/wali/children/" + $ac + "/previous-classes"), token: $wt},
  {method: "GET", path: ("/api/v1/wali/children/" + $rc + "/previous-classes"), token: $wt},
  {method: "GET", path: ("/api/v1/wali/children/" + $ac + "/previous-classes"), token: $gt},
  {method: "GET", path: "/api/v1/wali/children/not-a-uuid/previous-classes", token: $wt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $lk + "/students/" + $ac + "/history"), token: $mt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $lk + "/students/" + $ac + "/history"), token: $mt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $lk + "/students/" + $ac + "/history"), token: $gt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $lk + "/students/" + $ac + "/history"), token: $wt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $lk + "/students/" + $rc + "/history"), token: $mt},
  {method: "GET", path: ("/api/v1/guru/classes/" + $lk + "/students/not-a-uuid/history"), token: $mt},
  {method: "GET", path: ("/api/v1/guru/classes/not-a-uuid/students/" + $ac + "/history"), token: $mt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/archive"), token: $wt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/archive"), token: $mt},
  {method: "POST", path: "/api/v1/guru/classes/not-a-uuid/archive", token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/restore"), token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/archive"), token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/archive"), token: $gt},
  {method: "PUT", path: ("/api/v1/guru/classes/" + $o + "/joining"), token: $gt, body: {joinOpen: true}},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/restore"), token: $mt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/restore"), token: $gt},
  {method: "POST", path: ("/api/v1/guru/classes/" + $o + "/restore"), token: $gt},
  {method: "POST", path: "/api/v1/guru/classes/not-a-uuid/restore", token: $gt},
  {method: "GET", path: "/api/v1/guru/profile", token: $gt},
  {method: "GET", path: "/api/v1/guru/profile/detail", token: $gt},
  {method: "PUT", path: "/api/v1/guru/profile", token: $gt, body: {phoneNumber: "+62 812-0000-0000", address: "ZAP street 1"}},
  {method: "PUT", path: "/api/v1/guru/profile", token: $gt, body: {fullName: "ZAP Teacher"}},
  {method: "PATCH", path: "/api/v1/wali/profile", token: $wt, body: {fullName: "ZAP Parent", phoneNumber: "+62 812-0000-0001", whatsappNumber: "0812 0000 0002", address: "ZAP parent street 2"}},
  {method: "PATCH", path: "/api/v1/wali/profile", token: $wt, body: {phoneNumber: "not a phone"}},
  {method: "PATCH", path: "/api/v1/wali/profile", token: $wt, body: {}},
  {method: "PATCH", path: "/api/v1/wali/profile", token: $gt, body: {fullName: "wrong role"}},
  {method: "PATCH", path: ("/api/v1/wali/children/" + $s), token: $wt, body: {fullName: "ZAP Renamed Child"}},
  {method: "PATCH", path: ("/api/v1/wali/children/" + $s), token: $wt, body: {nisn: "not digits"}},
  {method: "PATCH", path: ("/api/v1/wali/children/" + $s), token: $wt, body: {}},
  {method: "PATCH", path: ("/api/v1/wali/children/" + $s), token: $gt, body: {fullName: "wrong role"}},
  {method: "PATCH", path: "/api/v1/wali/children/00000000-0000-4000-8000-000000000000", token: $wt, body: {fullName: "No Such Child"}},
  {method: "DELETE", path: "/api/v1/wali/children/00000000-0000-4000-8000-000000000000", token: $wt},
  {method: "DELETE", path: "/api/v1/wali/children/not-a-uuid", token: $wt},
  {method: "DELETE", path: ("/api/v1/wali/children/" + $s), token: $gt},
  {method: "GET", path: ("/api/v1/wali/child/" + $s), token: $wt},
  {method: "POST", path: "/api/v1/wali/guidance/apply", token: $wt, body: {guidanceId: "guidance-senang-4step", studentId: $s, parentNotes: "ZAP notes"}},
  {method: "GET", path: "/api/v1/trash/stats", token: $wt},
  {method: "POST", path: "/api/v1/trash/scan-claim", token: $wt, body: {studentId: $s, trashType: "ANORGANIK", confidenceScore: 0.9, photoBase64: $photo}},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, body: {studentId: $s, moodType: "SEDIH", recordedAt: $past}},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, body: {studentId: $s, moodType: "SEDIH", recordedAt: $future}},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, body: {studentId: $s, moodType: "SEDIH", recordedAt: $ancient}},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, body: {studentId: $s, moodType: "SEDIH", recordedAt: "yesterday"}},
  {method: "POST", path: "/api/v1/guru/mood/log/batch", token: $gt, body: {classId: $c, recordedAt: $past, entries: [{studentId: $s, moodType: "SENANG", confidenceScore: 0.9}]}},
  {method: "POST", path: "/api/v1/guru/mood/log/batch", token: $gt, body: {classId: $c, recordedAt: $future, entries: [{studentId: $s, moodType: "SENANG", confidenceScore: 0.9}]}},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, key: ("zapkey-" + $x + "-1"), body: {studentId: $s, moodType: "BINGUNG"}},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, key: ("zapkey-" + $x + "-1"), body: {studentId: $s, moodType: "BINGUNG"}},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, key: ("zapkey-" + $x + "-1"), body: {studentId: $s, moodType: "MARAH"}},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, key: "short", body: {studentId: $s, moodType: "BINGUNG"}},
  {method: "POST", path: "/api/v1/guru/mood/log", token: $gt, key: ("bad key with spaces " + $x), body: {studentId: $s, moodType: "BINGUNG"}},
  {method: "POST", path: ("/api/v1/classes/" + $c + "/forum"), token: $gt, key: ("zapkey-" + $x + "-2"), body: {title: "ZAP keyed thread", body: "ZAP keyed body"}},
  {method: "POST", path: ("/api/v1/classes/" + $c + "/forum"), token: $gt, key: ("zapkey-" + $x + "-2"), body: {title: "ZAP keyed thread", body: "ZAP keyed body"}},
  {method: "PUT", path: "/api/v1/users/me/password", token: $pw, body: {currentPassword: $p, newPassword: ($p + "-new")}},
  {method: "PUT", path: "/api/v1/users/me/password", token: $pw, body: {currentPassword: $p, newPassword: ($p + "-newer")}},
  {method: "PUT", path: "/api/v1/users/me/password", token: $pw, body: {currentPassword: ($p + "-new"), newPassword: ($p + "-new")}},
  {method: "PUT", path: "/api/v1/users/me/password", token: $pw, body: {currentPassword: ($p + "-new"), newPassword: "short"}},
  {method: "PUT", path: "/api/v1/users/me/password", token: $pw, body: {}},
  {method: "PUT", path: "/api/v1/users/me/password", token: $wt, body: {currentPassword: $w, newPassword: ($p + "-new")}},
  {method: "PUT", path: "/api/v1/users/me/password", body: {currentPassword: $p, newPassword: ($p + "-new")}},
  {method: "DELETE", path: "/api/v1/users/me", token: $lt, body: {password: $p}},
  {method: "POST", path: "/api/v1/auth/register", body: {email: ("zap.latecomer." + $x + "@example.com"), password: $p, fullName: "ZAP Latecomer", role: "WALI", classCode: $lc, student: {nisn: "423456789012", fullName: "ZAP Late Child"}}},
  {method: "DELETE", path: "/api/v1/users/me", token: $wt, body: {password: $w}}
]')"

# the account deletion above uses a wrong password on purpose, so the scan attacks the check without removing the account it needs
jq -n --argjson a "$requests" --argjson b "$extra" '$a + $b' >"$out"
