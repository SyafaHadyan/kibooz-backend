"""Smoke test of a deployed Kibooz API, started by hand from the Smoke workflow after a deploy.

It drives the public API with curl, so the user agent is curl's own, creates throwaway accounts and removes them again. One
keeper teacher stays behind, because deleting the last teacher of a class that has children needs a successor. The test never runs on pull requests.

Settings come from the environment
  BASE_URL               address of the API without a path, https://kibooz-api.syafahadyan.com by default. Plain http is only
                         accepted for 127.0.0.1, which is the throwaway stack that the Smoke stack workflow starts
  EXPECTED_VERSION       version that /healthz has to report, left out to skip that check
  SMOKE_KEEPER_EMAIL     the keeper teacher, registered by the first run when it does not exist yet
  SMOKE_KEEPER_PASSWORD  its password, and the object storage has to be configured for the upload checks
  SMOKE_STORAGE          off for a stack that has no bucket. The upload checks then expect STORAGE_UNAVAILABLE instead
"""
import atexit, base64, json, os, random, secrets, subprocess, sys, time, uuid
from datetime import datetime
from urllib.parse import quote, urlsplit

BASE = os.environ.get("BASE_URL", "https://kibooz-api.syafahadyan.com").rstrip("/")
API = BASE + "/api/v1"
EXPECTED_VERSION = os.environ.get("EXPECTED_VERSION", "")
KEEPER_EMAIL = os.environ.get("SMOKE_KEEPER_EMAIL", "")
KEEPER_PASSWORD = os.environ.get("SMOKE_KEEPER_PASSWORD", "")
PW = "Smoke-" + secrets.token_urlsafe(18) + "1!"
RUN = time.strftime("%m%d%H%M%S")
PNG = base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

STORAGE_ON = os.environ.get("SMOKE_STORAGE", "on") != "off"
# The address is parsed and not matched by its start, because curl reads the text before an @ as a login, so
# http://127.0.0.1:8080@other.example would pass a prefix check and send the tokens to another host
URL = urlsplit(BASE)
PLAIN_URL = URL.username is None and URL.password is None and not URL.path.strip("/") and not URL.query
LOCAL = PLAIN_URL and URL.scheme == "http" and URL.hostname == "127.0.0.1"
SECURE = PLAIN_URL and URL.scheme == "https" and bool(URL.hostname)

if not (SECURE or LOCAL) or not KEEPER_EMAIL or not KEEPER_PASSWORD:
    sys.exit("set BASE_URL (https, or http for 127.0.0.1 only), SMOKE_KEEPER_EMAIL and SMOKE_KEEPER_PASSWORD")

created = []  # every account of this run, so a crash can still clean up
passed, failed = 0, []


def finish():
    """Runs on every exit, a crash included. Removes the accounts of this run and writes the report."""
    for acc in created:
        if not acc.get("token") or acc.get("deleted"):
            continue
        extra = {"successorEmail": KEEPER_EMAIL} if acc["role"] == "GURU" else {}
        try:
            status, _ = delete_me(acc, timeout=15, **extra)
            if status != 200:
                failed.append(f"cleanup of {acc['email']} answered {status}, remove it by hand")
        except Exception as error:
            failed.append(f"cleanup of {acc['email']} raised {type(error).__name__}, remove it by hand")
    keeper = globals().get("g4") or {}
    lines = [f"Smoke test of {BASE} run {RUN}, passed {passed}, failed {len(failed)}"]
    if keeper:
        lines.append(f"Keeper {keeper['email']} {'reused' if keeper.get('reused') else 'registered now'}")
    lines += [f"FAIL {f}" for f in failed]
    print("\n".join(lines))
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a") as fh:
            fh.write("### Smoke test\n\n" + "\n".join(f"- {l}" for l in lines) + "\n")


def crashed(kind, error, trace):
    failed.append(f"the script stopped with {kind.__name__} {error}")
    sys.__excepthook__(kind, error, trace)


atexit.register(finish)
sys.excepthook = crashed


def curl(method, url, headers, data=None, timeout=60):
    cmd = ["curl", "-sS", "-X", method, url, "--max-time", str(timeout), "-w", "\n%{http_code}"]
    for k, v in headers.items():
        cmd += ["-H", f"{k}: {v}"]
    if data is not None:
        cmd += ["--data-binary", "@-"]
    out = subprocess.run(cmd, input=data, capture_output=True, check=False).stdout.decode(errors="replace")
    text, _, code = out.rpartition("\n")
    return int(code or 0), text


def call(method, path, token=None, body=None, raw=None, ctype=None, base=API, timeout=60):
    data = None
    headers = {"Accept": "application/json"}
    if raw is not None:
        data = raw
        headers["Content-Type"] = ctype
    elif body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    status, text = curl(method, base + path, headers, data, timeout)
    time.sleep(0.12)
    try:
        js = json.loads(text)
    except Exception:
        js = {"_raw": text[:200]}
    return status, js


def call_key(method, path, token, key, body):
    """A request with an Idempotency-Key. Returns the status, the body and whether the answer was a replay."""
    cmd = ["curl", "-sS", "-i", "-X", method, API + path, "--max-time", "60", "-H", "Accept: application/json",
           "-H", "Content-Type: application/json", "-H", "Authorization: Bearer " + token, "-H", "Idempotency-Key: " + key,
           "--data-binary", "@-"]
    out = subprocess.run(cmd, input=json.dumps(body).encode(), capture_output=True, check=False).stdout.decode(errors="replace")
    head, _, text = out.replace("\r\n", "\n").partition("\n\n")
    time.sleep(0.12)
    try:
        js = json.loads(text)
    except Exception:
        js = {"_raw": text[:200]}
    return int(head.split()[1]) if head.startswith("HTTP") else 0, js, "idempotent-replayed: true" in head.lower()


def dig(d, *keys):
    for k in keys:
        if isinstance(d, dict):
            d = d.get(k)
        elif isinstance(d, list) and isinstance(k, int) and -len(d) <= k < len(d):
            d = d[k]
        else:
            return None
    return d


def check(name, cond, detail=""):
    global passed
    if cond:
        passed += 1
    else:
        failed.append(f"{name} {detail}")


def expect(name, res, status, code=None):
    s, js = res
    ok = s == status and (code is None or js.get("errorCode") == code)
    check(name, ok, f"got {s} {js.get('errorCode')} {str(js.get('message'))[:80]} want {status} {code}")
    return js


def nisn():
    return "".join(random.choice("0123456789") for _ in range(10))


def register_guru(tag, cls):
    email = f"kibooz-smoke-{RUN}-{tag}@example.com"
    s, js = call("POST", "/auth/register", body={
        "email": email, "password": PW, "fullName": f"Smoke Guru {tag}", "role": "GURU",
        "nip": nisn() + nisn(), "class": {"name": cls, "gradeLevel": "Class A"}})
    check(f"register guru {tag}", s == 201, f"{s} {js}")
    d = js.get("data") or {}
    acc = {"email": email, "token": d.get("token"), "refresh": d.get("refreshToken"), "role": "GURU"}
    created.append(acc)
    s, dash = call("GET", "/guru/dashboard", acc["token"])
    acc["class"] = dig(dash, "data", "classOverview", "classId")
    acc["code"] = dig(dash, "data", "classOverview", "joinCode")
    acc["userId"] = None
    return acc


def register_wali(tag, code, child):
    email = f"kibooz-smoke-{RUN}-{tag}@example.com"
    s, js = call("POST", "/auth/register", body={
        "email": email, "password": PW, "fullName": f"Smoke Wali {tag}", "role": "WALI",
        "classCode": code, "student": {"nisn": nisn(), "fullName": child}})
    check(f"register wali {tag}", s == 201, f"{s} {js}")
    d = js.get("data") or {}
    acc = {"email": email, "token": d.get("token"), "refresh": d.get("refreshToken"), "role": "WALI"}
    created.append(acc)
    s, dash = call("GET", "/wali/dashboard", acc["token"])
    acc["student"] = dig(dash, "data", "student", "id")
    return acc


def delete_me(acc, timeout=60, **extra):
    body = {"password": acc.get("pw", PW)}
    body.update(extra)
    res = call("DELETE", "/users/me", acc["token"], body, timeout=timeout)
    if res[0] == 200:
        acc["deleted"] = True
    return res


# ---------- health and public ----------
s, js = call("GET", "/healthz", base=BASE)
check("healthz ok", s == 200 and js.get("status") == "ok", f"{s} {js}")
if EXPECTED_VERSION:
    check("healthz version", js.get("version") == EXPECTED_VERSION, f"{js.get('version')} want {EXPECTED_VERSION}")
want_checks = {"database": "ok", "redis": "ok", "storage": "ok" if STORAGE_ON else "disabled"}
check("healthz checks", (js.get("checks") or {}) == want_checks, str(js))
expect("unknown path 404", call("GET", "/nope"), 404, "NOT_FOUND")
for m, p in [("GET", "/wali/dashboard"), ("GET", "/guru/dashboard"), ("GET", "/leaderboard"), ("GET", "/leaderboard/history"),
             ("GET", "/trash/stats"), ("GET", "/guru/classes"), ("GET", "/guru/profile"), ("DELETE", "/users/me")]:
    expect(f"{m} {p} needs a token", call(m, p), 401, "AUTH_TOKEN_MISSING")
expect("bad token", call("GET", "/guru/dashboard", "not-a-token"), 401)

# ---------- registration ----------
g1 = register_guru("g1", "Smoke A")
g2 = register_guru("g2", "Smoke B")
g3 = register_guru("g3", "Smoke C")
ks, kj = call("POST", "/auth/login", body={"email": KEEPER_EMAIL, "password": KEEPER_PASSWORD, "role": "GURU"})
if ks == 200:
    g4 = {"email": KEEPER_EMAIL, "token": dig(kj, "data", "token"), "refresh": dig(kj, "data", "refreshToken"), "role": "GURU", "pw": KEEPER_PASSWORD}
    _, kd = call("GET", "/guru/dashboard", g4["token"])
    g4["class"] = dig(kd, "data", "classOverview", "classId")
    g4["code"] = dig(kd, "data", "classOverview", "joinCode")
    g4["reused"] = True
else:
    s, js = call("POST", "/auth/register", body={
        "email": KEEPER_EMAIL, "password": KEEPER_PASSWORD, "fullName": "Smoke Keeper", "role": "GURU",
        "nip": nisn() + nisn(), "class": {"name": "Smoke Keeper", "gradeLevel": "Class A"}})
    check("register the keeper", s == 201, f"{s} {js}")
    g4 = {"email": KEEPER_EMAIL, "token": dig(js, "data", "token"), "refresh": dig(js, "data", "refreshToken"), "role": "GURU", "pw": KEEPER_PASSWORD}
    _, kd = call("GET", "/guru/dashboard", g4["token"])
    g4["class"] = dig(kd, "data", "classOverview", "classId")
    g4["code"] = dig(kd, "data", "classOverview", "joinCode")
    g4["reused"] = False
_, kc = call("GET", "/guru/classes", g4["token"])
keeper_before = dig(kc, "data", "total") or 0
check("guru classes found", all(a["class"] and a["code"] for a in (g1, g2, g3, g4)))
check("join code has 8 chars", len(g1["code"] or "") == 8, g1["code"])

expect("duplicate email", call("POST", "/auth/register", body={
    "email": g1["email"], "password": PW, "fullName": "Dup Teacher", "role": "GURU", "nip": nisn() + "1",
    "class": {"name": "X"}}), 409, "EMAIL_ALREADY_REGISTERED")
expect("register validation", call("POST", "/auth/register", body={"email": "bad", "password": "x", "role": "GURU"}), 400, "VALIDATION_ERROR")
expect("register unknown code", call("POST", "/auth/register", body={
    "email": f"kibooz-smoke-{RUN}-zz@example.com", "password": PW, "fullName": "No Code", "role": "WALI",
    "classCode": "ZZZZZZZZ", "student": {"nisn": nisn(), "fullName": "Nobody"}}), 404, "CLASS_CODE_NOT_FOUND")

w1 = register_wali("w1", g1["code"], "Smoke Child One")
w2 = register_wali("w2", g1["code"], "Smoke Child Two")
check("wali student ids", bool(w1["student"] and w2["student"]))
expect("duplicate nisn", call("POST", "/auth/register", body={
    "email": f"kibooz-smoke-{RUN}-dn@example.com", "password": PW, "fullName": "Dup Nisn", "role": "WALI",
    "classCode": g1["code"], "student": {"nisn": "0", "fullName": "Short Nisn"}}), 400, "VALIDATION_ERROR")

# ---------- login, refresh, logout ----------
s, js = call("POST", "/auth/login", body={"email": g1["email"], "password": PW, "role": "GURU"})
check("login ok", s == 200 and dig(js, "data", "token"), f"{s} {js}")
expect("login wrong password", call("POST", "/auth/login", body={"email": g1["email"], "password": PW + "x", "role": "GURU"}), 401, "AUTH_INVALID_CREDENTIALS")
expect("login wrong role", call("POST", "/auth/login", body={"email": g1["email"], "password": PW, "role": "WALI"}), 401, "AUTH_INVALID_CREDENTIALS")
old_refresh = dig(js, "data", "refreshToken")
s, js2 = call("POST", "/auth/refresh-token", body={"refreshToken": old_refresh, "email": g1["email"]})
check("refresh rotates", s == 200 and dig(js2, "data", "refreshToken") not in (None, old_refresh), f"{s} {js2}")
expect("refresh reuse rejected", call("POST", "/auth/refresh-token", body={"refreshToken": old_refresh, "email": g1["email"]}), 401, "AUTH_REFRESH_INVALID")
s, js3 = call("POST", "/auth/login", body={"email": g1["email"], "password": PW, "role": "GURU"})
lr = dig(js3, "data", "refreshToken")
expect("logout", call("POST", "/auth/logout", body={"refreshToken": lr}), 200)
expect("refresh after logout", call("POST", "/auth/refresh-token", body={"refreshToken": lr, "email": g1["email"]}), 401, "AUTH_REFRESH_INVALID")

# ---------- dashboards, roles ----------
expect("wali dashboard", call("GET", "/wali/dashboard", w1["token"]), 200)
expect("guru dashboard", call("GET", "/guru/dashboard", g1["token"]), 200)
expect("wali cannot open guru dashboard", call("GET", "/guru/dashboard", w1["token"]), 403, "AUTH_FORBIDDEN")
expect("guru cannot open wali dashboard", call("GET", "/wali/dashboard", g1["token"]), 403, "AUTH_FORBIDDEN")
expect("mood analytics", call("GET", "/guru/mood/analytics", g1["token"]), 200)

# ---------- mood ----------
expect("log mood", call("POST", "/guru/mood/log", g1["token"], {"studentId": w1["student"], "moodType": "BINGUNG", "notes": "api test"}), 201)
expect("log mood invalid enum", call("POST", "/guru/mood/log", g1["token"], {"studentId": w1["student"], "moodType": "LAPAR"}), 400, "VALIDATION_ERROR")
s, js = call("POST", "/guru/mood/log", g2["token"], {"studentId": w1["student"], "moodType": "SENANG"})
check("other class teacher cannot log mood", s in (403, 404), f"{s} {js.get('errorCode')}")
expect("wali cannot log mood", call("POST", "/guru/mood/log", w1["token"], {"studentId": w1["student"], "moodType": "SENANG"}), 403, "AUTH_FORBIDDEN")
s, js = call("GET", "/wali/dashboard", w1["token"])
guide = dig(js, "data", "recommendedGuidance", "id")
check("guidance recommended after BINGUNG", guide == "guidance-bingung-4step", str(guide))

# ---------- guidance ----------
expect("apply guidance", call("POST", "/wali/guidance/apply", w1["token"], {"guidanceId": guide or "guidance-bingung-4step", "studentId": w1["student"], "parentNotes": "tried it"}), 200)
expect("apply unknown guidance", call("POST", "/wali/guidance/apply", w1["token"], {"guidanceId": "not-found", "studentId": w1["student"]}), 404, "GUIDANCE_NOT_FOUND")
s, js = call("GET", f"/classes/{g1['class']}/guidance-reports", g1["token"])
check("guru reads guidance reports", s == 200 and dig(js, "data", "total") == 1, f"{s} {js}")
expect("wali cannot read guidance reports", call("GET", f"/classes/{g1['class']}/guidance-reports", w1["token"]), 403, "AUTH_FORBIDDEN")

# ---------- trash and leaderboard ----------
expect("scan claim", call("POST", "/trash/scan-claim", w1["token"], {"studentId": w1["student"], "trashType": "ORGANIK", "confidenceScore": 0.9}), 200)
expect("scan claim second", call("POST", "/trash/scan-claim", w2["token"], {"studentId": w2["student"], "trashType": "B3", "confidenceScore": 0.8}), 200)
expect("scan claim invalid type", call("POST", "/trash/scan-claim", w1["token"], {"studentId": w1["student"], "trashType": "KACA"}), 400, "VALIDATION_ERROR")
expect("scan claim other child", call("POST", "/trash/scan-claim", w1["token"], {"studentId": w2["student"], "trashType": "ORGANIK"}), 404, "STUDENT_NOT_FOUND")
s, js = call("GET", "/trash/stats", w1["token"])
check("trash stats", s == 200 and dig(js, "data", "seasonPoints") is not None and len(dig(js, "data", "breakdown") or []) == 3, f"{s} {js}")
s, js = call("GET", "/leaderboard", w1["token"])
names = [e.get("studentName") for e in (dig(js, "data", "podium") or []) + (dig(js, "data", "rankings") or [])]
check("leaderboard lists both children", s == 200 and "Smoke Child One" in names and "Smoke Child Two" in names, f"{s} {names}")
expect("leaderboard by class (guru)", call("GET", f"/leaderboard?classId={g1['class']}", g1["token"]), 200)
s, js = call("GET", "/leaderboard/history", w1["token"])
check("history empty for a young class", s == 200 and dig(js, "data", "seasons") == [] and dig(js, "data", "total") == 0, f"{s} {js}")
check("history page info", dig(js, "data", "page") == 1 and dig(js, "data", "limit") == 20)
expect("history by class (guru)", call("GET", f"/leaderboard/history?classId={g1['class']}&page=1&limit=5", g1["token"]), 200)
expect("history bad class id", call("GET", "/leaderboard/history?classId=nope", g1["token"]), 400, "VALIDATION_ERROR")
expect("history bad page", call("GET", "/leaderboard/history?page=0", g1["token"]), 400, "VALIDATION_ERROR")
expect("history bad limit", call("GET", "/leaderboard/history?limit=500", g1["token"]), 400, "VALIDATION_ERROR")
s, js = call("GET", f"/leaderboard/history?classId={g1['class']}", g2["token"])
check("history of a class you do not teach", s in (403, 404), f"{s} {js.get('errorCode')}")
s, js = call("GET", f"/leaderboard/history?classId={g1['class']}", w1["token"])
check("history parent own class", s == 200)

# ---------- classes ----------
s, js = call("GET", "/guru/classes", g1["token"])
check("classes list has the registration class", s == 200 and dig(js, "data", "total") == 1 and dig(js, "data", "classes", 0, "totalStudents") == 2, f"{s} {js}")
s, js = call("POST", "/guru/classes", g1["token"], {"name": "Smoke A2", "gradeLevel": "Class B"})
check("create class", s == 201 and dig(js, "data", "joinCode"), f"{s} {js}")
g1_second = dig(js, "data", "id")
g1_second_code = dig(js, "data", "joinCode")
expect("create class validation", call("POST", "/guru/classes", g1["token"], {"name": ""}), 400, "VALIDATION_ERROR")
s, js = call("GET", f"/guru/classes/{g1['class']}", g1["token"])
check("class detail counts", s == 200 and dig(js, "data", "totalStudents") == 2 and dig(js, "data", "totalVideos") == 0, f"{s} {js}")
expect("class detail other teacher", call("GET", f"/guru/classes/{g1['class']}", g2["token"]), 404, "CLASS_NOT_FOUND")
s, js = call("POST", f"/guru/classes/{g1['class']}/join-code", g1["token"])
new_code = dig(js, "data", "joinCode")
check("replace join code", s == 200 and new_code and new_code != g1["code"], f"{s} {js}")
expect("old code no longer works", call("POST", "/auth/register", body={
    "email": f"kibooz-smoke-{RUN}-oc@example.com", "password": PW, "fullName": "Old Code", "role": "WALI", "classCode": g1["code"],
    "student": {"nisn": nisn(), "fullName": "Old Code Child"}}), 404, "CLASS_CODE_NOT_FOUND")
g1["code"] = new_code
s, js = call("GET", f"/classes/{g1['class']}/students", g1["token"])
check("roster", s == 200 and dig(js, "data", "total") == 2, f"{s} {js}")
expect("roster closed to parents", call("GET", f"/classes/{g1['class']}/students", w1["token"]), 403, "AUTH_FORBIDDEN")

# ---------- wali children ----------
s, js = call("GET", "/wali/children", w1["token"])
check("children list", s == 200 and len(dig(js, "data", "children") or []) == 1, f"{s} {js}")
s, js = call("POST", "/wali/children", w1["token"], {"classCode": g2["code"], "student": {"nisn": nisn(), "fullName": "Smoke Child Three"}})
check("add child in another class", s == 201, f"{s} {js}")
expect("child limit etc validation", call("POST", "/wali/children", w1["token"], {"classCode": "ZZZZZZZZ", "student": {"nisn": nisn(), "fullName": "Nope Child"}}), 404, "CLASS_CODE_NOT_FOUND")
expect("child profile", call("GET", f"/wali/child/{w1['student']}", w1["token"]), 200)
expect("other parents child profile", call("GET", f"/wali/child/{w2['student']}", w1["token"]), 404, "STUDENT_NOT_FOUND")

# ---------- 0.11.0 batch mood log, the route does not exist in older versions ----------
BATCH = "/guru/mood/log/batch"


def entry(student, mood, score=0.9, **extra):
    return {"studentId": student, "moodType": mood, "confidenceScore": score, **extra}


def batch(token, entries, class_id=None):
    return call("POST", BATCH, token, {"classId": class_id or g1["class"], "entries": entries})


s, js = batch(g1["token"], [entry(w1["student"], "BINGUNG", 0.88), {"studentId": w2["student"], "moodType": "SENANG", "source": "MANUAL_INPUT", "notes": "smoke"}])
logs = dig(js, "data", "logs") or []
check("batch saves every entry", s == 201 and len(logs) == 2, f"{s} {js}")
check("batch keeps the request order", [l.get("studentId") for l in logs] == [w1["student"], w2["student"]], str(logs))
check("batch shares one recordedAt", len(logs) == 2 and len({l.get("recordedAt") for l in logs}) == 1, str(logs))
expect("batch without entries", batch(g1["token"], []), 400, "VALIDATION_ERROR")
expect("batch with too many entries", batch(g1["token"], [entry(w1["student"], "SENANG")] * 31), 400, "VALIDATION_ERROR")
expect("batch camera entry without a score", batch(g1["token"], [{"studentId": w1["student"], "moodType": "SENANG"}]), 400, "VALIDATION_ERROR")
expect("batch unknown mood", batch(g1["token"], [entry(w1["student"], "LAPAR")]), 400, "VALIDATION_ERROR")
expect("batch score above one", batch(g1["token"], [entry(w1["student"], "SENANG", 1.5)]), 400, "VALIDATION_ERROR")
expect("batch repeated child", batch(g1["token"], [entry(w1["student"], "SENANG"), entry(w1["student"], "SEDIH")]), 400, "VALIDATION_ERROR")
expect("batch unknown child", batch(g1["token"], [entry(str(uuid.uuid4()), "SENANG")]), 400, "VALIDATION_ERROR")
expect("batch without a class", call("POST", BATCH, g1["token"], {"entries": [entry(w1["student"], "SENANG")]}), 400, "VALIDATION_ERROR")
expect("batch needs a token", call("POST", BATCH, None, {"classId": g1["class"], "entries": [entry(w1["student"], "SENANG")]}), 401, "AUTH_TOKEN_MISSING")
expect("parent cannot save a batch", batch(w1["token"], [entry(w1["student"], "SENANG")]), 403, "AUTH_FORBIDDEN")
before_status, before = call("GET", f"/guru/dashboard?classId={g1['class']}", g1["token"])
expect("teacher of another class cannot save a batch", batch(g2["token"], [entry(w1["student"], "SENANG")], g1["class"]), 404, "CLASS_NOT_FOUND")
after_status, after = call("GET", f"/guru/dashboard?classId={g1['class']}", g1["token"])
check("a refused batch changes no mood", before_status == 200 and after_status == 200 and dig(before, "data", "dailyMoodDistribution") is not None
      and dig(before, "data", "dailyMoodDistribution") == dig(after, "data", "dailyMoodDistribution"), f"{before_status} {after_status}")
expect("batch for a class that does not exist", batch(g1["token"], [entry(w1["student"], "SENANG")], str(uuid.uuid4())), 404, "CLASS_NOT_FOUND")

# ---------- profile ----------
expect("guru profile", call("GET", "/guru/profile", g1["token"]), 200)
expect("guru profile update", call("PUT", "/guru/profile", g1["token"], {"phoneNumber": "+62 812 3456 7890"}), 200)
expect("guru profile bad phone", call("PUT", "/guru/profile", g1["token"], {"phoneNumber": "abc"}), 400, "VALIDATION_ERROR")
expect("guru profile detail", call("GET", "/guru/profile/detail", g1["token"]), 200)
# since 0.13.0 a teacher and a parent can change their name, and a parent can change the contact details
s, js = call("PUT", "/guru/profile", g1["token"], {"fullName": "Smoke Teacher Renamed"})
check("teacher changes the name", s == 200 and dig(js, "data", "fullName") == "Smoke Teacher Renamed", f"{s} {js}")
expect("teacher name cannot be empty", call("PUT", "/guru/profile", g1["token"], {"fullName": "  "}), 400, "VALIDATION_ERROR")
s, js = call("PATCH", "/wali/profile", w1["token"], {"fullName": "Smoke Parent Renamed", "phoneNumber": "+62 812 3456 7890", "whatsappNumber": "0812 3456 7891", "address": "Smoke Street 1"})
check("parent changes the profile", s == 200 and dig(js, "data", "fullName") == "Smoke Parent Renamed" and dig(js, "data", "address") == "Smoke Street 1", f"{s} {js}")
s, js = call("PATCH", "/wali/profile", w1["token"], {"whatsappNumber": ""})
check("an empty text clears a parent field and keeps the others", s == 200 and dig(js, "data", "whatsappNumber") is None and dig(js, "data", "phoneNumber") == "+62 812 3456 7890", f"{s} {js}")
expect("parent bad phone", call("PATCH", "/wali/profile", w1["token"], {"phoneNumber": "abc"}), 400, "VALIDATION_ERROR")
expect("parent empty body", call("PATCH", "/wali/profile", w1["token"], {}), 400, "VALIDATION_ERROR")
expect("a teacher cannot use the parent profile", call("PATCH", "/wali/profile", g1["token"], {"fullName": "x"}), 403, "AUTH_FORBIDDEN")
# since 0.13.0 a parent can change the name and the NISN of a child
s, js = call("PATCH", f"/wali/children/{w1['student']}", w1["token"], {"fullName": "Smoke Child One Renamed"})
check("parent renames a child", s == 200 and dig(js, "data", "fullName") == "Smoke Child One Renamed", f"{s} {js}")
new_nisn = nisn()
s, js = call("PATCH", f"/wali/children/{w1['student']}", w1["token"], {"nisn": new_nisn})
check("parent changes the NISN of a child", s == 200 and dig(js, "data", "nisn") == new_nisn, f"{s} {js}")
s, js = call("GET", f"/wali/child/{w2['student']}", w2["token"])
expect("a NISN of another child is refused", call("PATCH", f"/wali/children/{w1['student']}", w1["token"], {"nisn": dig(js, "data", "nisn")}), 409, "NISN_ALREADY_REGISTERED")
expect("a child edit needs a field", call("PATCH", f"/wali/children/{w1['student']}", w1["token"], {}), 400, "VALIDATION_ERROR")
expect("another parent cannot change the child", call("PATCH", f"/wali/children/{w1['student']}", w2["token"], {"fullName": "Nope Child"}), 404, "STUDENT_NOT_FOUND")
# since 0.13.0 a parent can delete a child, also the last one. w8 is a parent that only exists for this check.
w8 = register_wali("w8", g1["code"], "Smoke Child To Delete")
expect("another parent cannot delete the child", call("DELETE", f"/wali/children/{w8['student']}", w2["token"]), 404, "STUDENT_NOT_FOUND")
expect("delete with a bad id", call("DELETE", "/wali/children/nope", w8["token"]), 400, "VALIDATION_ERROR")
expect("a teacher cannot delete a child as a parent", call("DELETE", f"/wali/children/{w8['student']}", g1["token"]), 403, "AUTH_FORBIDDEN")
expect("a parent deletes the last child", call("DELETE", f"/wali/children/{w8['student']}", w8["token"]), 200)
expect("deleting it again is not found", call("DELETE", f"/wali/children/{w8['student']}", w8["token"]), 404, "STUDENT_NOT_FOUND")
expect("a parent without a child has a clear dashboard answer", call("GET", "/wali/dashboard", w8["token"]), 404, "NO_CHILD_REGISTERED")
s, js = call("GET", "/wali/children", w8["token"])
check("a parent without a child lists none", s == 200 and dig(js, "data", "children") == [], f"{s} {js}")

# ---------- avatar ----------
boundary = "----smoke" + uuid.uuid4().hex
mp = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\nContent-Type: image/png\r\n\r\n").encode() + PNG + f"\r\n--{boundary}--\r\n".encode()
s, js = call("POST", "/users/avatar", g1["token"], raw=mp, ctype="multipart/form-data; boundary=" + boundary)
if STORAGE_ON:
    check("avatar upload", s == 200 and dig(js, "data", "avatarUrl"), f"{s} {js}")
    mp2 = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\nContent-Type: image/png\r\n\r\n").encode() + b"not an image" + f"\r\n--{boundary}--\r\n".encode()
    expect("avatar rejects non image", call("POST", "/users/avatar", g1["token"], raw=mp2, ctype="multipart/form-data; boundary=" + boundary), 400, "INVALID_IMAGE")
else:
    check("avatar upload without storage", s == 503 and js.get("errorCode") == "STORAGE_UNAVAILABLE", f"{s} {js}")

# ---------- videos ----------
cid = g1["class"]
s, js = call("POST", f"/classes/{cid}/videos", g1["token"], {"title": "Smoke Video", "videoUrl": "https://example.com/video.mp4", "durationSeconds": 60})
vid = dig(js, "data", "id")
check("add video", s == 201 and vid, f"{s} {js}")
expect("add video http rejected", call("POST", f"/classes/{cid}/videos", g1["token"], {"title": "x", "videoUrl": "http://example.com/v.mp4"}), 400, "VALIDATION_ERROR")
expect("parent cannot add video", call("POST", f"/classes/{cid}/videos", w1["token"], {"title": "x", "videoUrl": "https://example.com/v.mp4"}), 403, "AUTH_FORBIDDEN")
s, js = call("GET", f"/classes/{cid}/videos", w1["token"])
check("parent lists videos", s == 200 and dig(js, "data", "total") == 1, f"{s} {js}")
expect("edit video", call("PATCH", f"/classes/{cid}/videos/{vid}", g1["token"], {"title": "Smoke Video Edited"}), 200)
expect("edit video empty body", call("PATCH", f"/classes/{cid}/videos/{vid}", g1["token"], {}), 400, "VALIDATION_ERROR")
s, js = call("POST", f"/classes/{cid}/videos/upload-url", g1["token"], {"contentType": "video/mp4", "sizeBytes": 1048576})
if STORAGE_ON:
    check("upload url issued", s == 200 and dig(js, "data", "uploadUrl"), f"{s} {js}")
else:
    check("upload url without storage", s == 503 and js.get("errorCode") == "STORAGE_UNAVAILABLE", f"{s} {js}")
expect("upload url bad type", call("POST", f"/classes/{cid}/videos/upload-url", g1["token"], {"contentType": "video/avi", "sizeBytes": 1000}), 400, "VALIDATION_ERROR")

# ---------- 0.10.0 delete a pending upload ----------
# the bucket is needed for every check here, so a stack without one skips them
if STORAGE_ON:
    UP = f"/classes/{cid}/videos/upload-url"


    def discard(token, url):
        return call("DELETE", UP + "?videoUrl=" + quote(url, safe=""), token)


    def send_file(up, size):
        if not dig(up, "data", "uploadUrl"):
            return 0, ""
        return curl("PUT", dig(up, "data", "uploadUrl"), dig(up, "data", "headers"), b"\0" * size)


    s, up = call("POST", UP, g1["token"], {"contentType": "video/mp4", "sizeBytes": 64})
    check("upload address for a discard", s == 200 and dig(up, "data", "videoUrl"), f"{s} {up}")
    pending_url = dig(up, "data", "videoUrl") or ""
    check("pending address is under pending/", "/pending/videos/" in (pending_url or ""), str(pending_url))
    ps, _ = send_file(up, 64)
    check("file arrives in the bucket", ps == 200, str(ps))
    expect("discard needs a token", call("DELETE", UP + "?videoUrl=" + quote(pending_url, safe="")), 401, "AUTH_TOKEN_MISSING")
    expect("parent cannot discard", discard(w1["token"], pending_url), 403, "AUTH_FORBIDDEN")
    expect("other class teacher cannot discard", discard(g2["token"], pending_url), 403, "AUTH_FORBIDDEN")
    expect("discard without videoUrl", call("DELETE", UP, g1["token"]), 400, "VALIDATION_ERROR")
    expect("discard a link to another site", discard(g1["token"], "https://example.com/video.mp4"), 400, "VALIDATION_ERROR")
    expect("discard bad class id", call("DELETE", "/classes/nope/videos/upload-url?videoUrl=x", g1["token"]), 400, "VALIDATION_ERROR")
    s, other = call("POST", f"/classes/{g2['class']}/videos/upload-url", g2["token"], {"contentType": "video/mp4", "sizeBytes": 64})
    other_url = dig(other, "data", "videoUrl") or ""
    expect("discard a file of another class", discard(g1["token"], other_url), 400, "VALIDATION_ERROR")
    expect("discard a file that waits", discard(g1["token"], pending_url), 200)
    expect("discard twice is fine", discard(g1["token"], pending_url), 200)
    late = call("POST", f"/classes/{cid}/videos", g1["token"], {"title": "late", "videoUrl": pending_url})
    expect("a discarded file cannot be added", late, 400, "VALIDATION_ERROR")

    # a real upload added as a video, then the permanent address is refused by the discard route
    s, up2 = call("POST", UP, g1["token"], {"contentType": "video/mp4", "sizeBytes": 64})
    send_file(up2, 64)
    s, added = call("POST", f"/classes/{cid}/videos", g1["token"], {"title": "Smoke Upload", "videoUrl": dig(up2, "data", "videoUrl")})
    upload_vid = dig(added, "data", "id")
    permanent = dig(added, "data", "videoUrl") or ""
    check("uploaded file added as a video", s == 201 and upload_vid and "/pending/" not in permanent, f"{s} {added}")
    expect("discard refuses an added video", discard(g1["token"], permanent), 400, "VALIDATION_ERROR")
    expect("delete the uploaded video", call("DELETE", f"/classes/{cid}/videos/{upload_vid}", g1["token"]), 200)
    expect("their own teacher discards the file of the other class", call("DELETE", f"/classes/{g2['class']}/videos/upload-url?videoUrl=" + quote(other_url, safe=""), g2["token"]), 200)
expect("delete video", call("DELETE", f"/classes/{cid}/videos/{vid}", g1["token"]), 200)
expect("deleted video gone", call("PATCH", f"/classes/{cid}/videos/{vid}", g1["token"], {"title": "x"}), 404, "VIDEO_NOT_FOUND")
expect("other class videos closed", call("GET", f"/classes/{cid}/videos", g2["token"]), 403, "AUTH_FORBIDDEN")

# ---------- 0.13.0 class limit of a teacher ----------
g6 = register_guru("g6", "Smoke Limit 1")
limit_class = None
for i in range(2, 23):
    s, js = call("POST", "/guru/classes", g6["token"], {"name": f"Smoke Limit {i}"})
    if s != 201:
        limit_class = (i, s, js.get("errorCode"))
        break
check("a teacher stops creating classes at 20", limit_class == (21, 409, "CLASS_LIMIT_REACHED"), str(limit_class))
expect("the teacher with 20 classes deletes without a successor", delete_me(g6), 200)

# ---------- 0.13.0 closing a class for new children ----------
joining = f"/guru/classes/{g1['class']}/joining"
s, js = call("GET", f"/guru/classes/{g1['class']}", g1["token"])
check("a class starts open", s == 200 and dig(js, "data", "joinOpen") is True, f"{s} {js}")
expect("joining needs a boolean", call("PUT", joining, g1["token"], {}), 400, "VALIDATION_ERROR")
expect("a parent cannot close a class", call("PUT", joining, w1["token"], {"joinOpen": False}), 403, "AUTH_FORBIDDEN")
s, js = call("PUT", joining, g1["token"], {"joinOpen": False})
check("close a class", s == 200 and dig(js, "data", "joinOpen") is False, f"{s} {js}")
expect("a closed class refuses a new parent", call("POST", "/auth/register", body={
    "email": f"kibooz-smoke-{RUN}-cl@example.com", "password": PW, "fullName": "Closed Parent", "role": "WALI", "classCode": g1["code"],
    "student": {"nisn": nisn(), "fullName": "Closed Child"}}), 409, "CLASS_JOINING_CLOSED")
expect("a closed class refuses another child", call("POST", "/wali/children", w1["token"], {
    "classCode": g1["code"], "student": {"nisn": nisn(), "fullName": "Closed Second Child"}}), 409, "CLASS_JOINING_CLOSED")
s, js = call("GET", f"/guru/classes/{g1['class']}", g1["token"])
check("closing keeps the code and shows joinOpen", s == 200 and dig(js, "data", "joinOpen") is False and dig(js, "data", "joinCode") == g1["code"], f"{s} {js}")
s, js = call("PUT", joining, g1["token"], {"joinOpen": True})
check("open a class again", s == 200 and dig(js, "data", "joinOpen") is True, f"{s} {js}")

# ---------- 0.13.0 removing a child from a class ----------
w7 = register_wali("w7", g1["code"], "Smoke Child To Remove")
removal = f"/guru/classes/{g1['class']}/students/{w7['student']}"
s, js = call("GET", f"/classes/{g1['class']}/students", g1["token"])
roster_before = dig(js, "data", "total")
expect("a parent cannot remove a child", call("DELETE", removal, w1["token"]), 403, "AUTH_FORBIDDEN")
expect("a teacher of another class cannot remove the child", call("DELETE", f"/guru/classes/{g2['class']}/students/{w7['student']}", g2["token"]), 404, "STUDENT_NOT_FOUND")
expect("remove a child with a bad id", call("DELETE", f"/guru/classes/{g1['class']}/students/nope", g1["token"]), 400, "VALIDATION_ERROR")
expect("remove a child", call("DELETE", removal, g1["token"]), 200)
s, js = call("GET", f"/classes/{g1['class']}/students", g1["token"])
check("the roster lost the child", s == 200 and roster_before is not None and dig(js, "data", "total") == roster_before - 1, f"{s} {js} before={roster_before}")
expect("removing the child again is not found", call("DELETE", removal, g1["token"]), 404, "STUDENT_NOT_FOUND")
expect("the removed child is gone for the parent", call("GET", f"/wali/child/{w7['student']}", w7["token"]), 404, "STUDENT_NOT_FOUND")

# ---------- forum ----------
s, js = call("POST", f"/classes/{cid}/forum", w1["token"], {"title": "Smoke thread", "body": "hello"})
pid = dig(js, "data", "id")
check("parent starts thread", s == 201 and pid, f"{s} {js}")
s, js = call("POST", f"/classes/{cid}/forum/{pid}/replies", g1["token"], {"body": "reply from teacher"})
rid = dig(js, "data", "id")
check("teacher replies", s == 201 and rid, f"{s} {js}")
s, js = call("GET", f"/classes/{cid}/forum", w2["token"])
check("other parent lists threads", s == 200 and dig(js, "data", "total") == 1, f"{s} {js}")
s, js = call("GET", f"/classes/{cid}/forum/{pid}/replies", w2["token"])
check("list replies", s == 200 and dig(js, "data", "total") == 1, f"{s} {js}")
s, js = call("PATCH", f"/classes/{cid}/forum/{pid}", w1["token"], {"title": "Smoke thread edited"})
check("author edits thread", s == 200 and dig(js, "data", "editedAt"), f"{s} {js}")
expect("other parent cannot edit thread", call("PATCH", f"/classes/{cid}/forum/{pid}", w2["token"], {"title": "hijack"}), 403, "AUTH_FORBIDDEN")
expect("author edits reply", call("PATCH", f"/classes/{cid}/forum/{pid}/replies/{rid}", g1["token"], {"body": "edited"}), 200)
expect("outsider cannot read forum", call("GET", f"/classes/{cid}/forum", g2["token"]), 403, "AUTH_FORBIDDEN")
expect("delete reply", call("DELETE", f"/classes/{cid}/forum/{pid}/replies/{rid}", g1["token"]), 200)
expect("teacher deletes thread", call("DELETE", f"/classes/{cid}/forum/{pid}", g1["token"]), 200)
expect("deleted thread gone", call("GET", f"/classes/{cid}/forum/{pid}/replies", g1["token"]), 404, "FORUM_POST_NOT_FOUND")

# ---------- chat ----------
# The chat has a teacher and two parents of its own. The requests of g1 and w1 are counted by the limiter of the user, and the
# checks around here already use most of it.
gc = register_guru("gc", "Smoke Chat")
wc1 = register_wali("wc1", gc["code"], "Smoke Chat Child One")
wc2 = register_wali("wc2", gc["code"], "Smoke Chat Child Two")


def contact_named(js, name):
    return next((c for c in dig(js, "data", "contacts") or [] if c.get("fullName") == name), None)


s, js = call("GET", "/chat/contacts", gc["token"])
wc1_contact = contact_named(js, "Smoke Wali wc1")
check("teacher sees the parent as a contact", s == 200 and wc1_contact is not None and wc1_contact.get("children") == ["Smoke Chat Child One"]
      and wc1_contact.get("conversationId") is None and dig(js, "data", "total") == 2, f"{s} {js}")
s, js = call("GET", "/chat/contacts", wc1["token"])
gc_contact = contact_named(js, "Smoke Guru gc")
check("parent sees the teacher as a contact", s == 200 and gc_contact is not None and dig(js, "data", "total") == 1, f"{s} {js}")
wc1_id, gc_id = (wc1_contact or {}).get("id"), (gc_contact or {}).get("id")
s, js = call("POST", "/chat/conversations", wc1["token"], {"userId": gc_id})
conv = dig(js, "data", "id")
check("parent starts a conversation", s == 201 and conv and dig(js, "data", "lastMessage") is None and dig(js, "data", "canSend") is True, f"{s} {js}")
s, js = call("POST", "/chat/conversations", gc["token"], {"userId": wc1_id})
check("the teacher reaches the same conversation", s == 200 and dig(js, "data", "id") == conv, f"{s} {js}")
expect("two parents cannot talk in private", call("POST", "/chat/conversations", wc2["token"], {"userId": wc1_id}), 403, "CHAT_CONTACT_NOT_ALLOWED")
expect("a conversation with nobody", call("POST", "/chat/conversations", wc1["token"], {"userId": str(uuid.uuid4())}), 403, "CHAT_CONTACT_NOT_ALLOWED")

chat_key = str(uuid.uuid4())
s, js, replayed = call_key("POST", f"/chat/conversations/{conv}/messages", wc1["token"], chat_key, {"body": "  Good morning  "})
first_message = dig(js, "data", "id")
check("parent sends a message", s == 201 and first_message and dig(js, "data", "body") == "Good morning" and not replayed, f"{s} {js}")
s, js, replayed = call_key("POST", f"/chat/conversations/{conv}/messages", wc1["token"], chat_key, {"body": "  Good morning  "})
check("repeating the key replays the message", s == 201 and dig(js, "data", "id") == first_message and replayed, f"{s} {js} {replayed}")
s, js = call("POST", f"/chat/conversations/{conv}/messages", gc["token"], {"body": "Good morning, how is the child?"})
reply_message = dig(js, "data", "id")
check("teacher replies", s == 201 and reply_message and first_message and reply_message > first_message, f"{s} {js}")
expect("blank message is refused", call("POST", f"/chat/conversations/{conv}/messages", gc["token"], {"body": "   "}), 400, "VALIDATION_ERROR")

s, js = call("GET", "/chat/conversations", wc1["token"])
check("the conversation is listed with its unread count", s == 200 and dig(js, "data", "total") == 1
      and dig(js, "data", "conversations", 0, "unreadCount") == 1 and dig(js, "data", "conversations", 0, "lastMessage", "id") == reply_message, f"{s} {js}")
s, js = call("GET", f"/chat/conversations/{conv}/messages?after=0", wc1["token"])
check("the messages come in the order they were sent", s == 200 and [m.get("id") for m in dig(js, "data", "messages") or []] == [first_message, reply_message]
      and dig(js, "data", "hasMore") is False, f"{s} {js}")
s, js = call("PUT", f"/chat/conversations/{conv}/read", wc1["token"], {"messageId": reply_message})
check("parent marks the conversation read", s == 200 and dig(js, "data", "lastReadMessageId") == reply_message and dig(js, "data", "unreadCount") == 0, f"{s} {js}")
expect("a teacher of another class cannot read it", call("GET", f"/chat/conversations/{conv}/messages", g2["token"]), 404, "CHAT_CONVERSATION_NOT_FOUND")
expect("another parent cannot read it", call("GET", f"/chat/conversations/{conv}/messages", wc2["token"]), 404, "CHAT_CONVERSATION_NOT_FOUND")
expect("only the sender deletes a message", call("DELETE", f"/chat/conversations/{conv}/messages/{reply_message}", wc1["token"]), 403, "AUTH_FORBIDDEN")
s, js = call("DELETE", f"/chat/conversations/{conv}/messages/{reply_message}", gc["token"])
check("the sender deletes a message", s == 200 and dig(js, "data", "deleted") is True and dig(js, "data", "body") == "", f"{s} {js}")

# ---------- 0.9.0 teachers, owner, leaving ----------
tb = f"/guru/classes/{cid}/teachers"
s, js = call("GET", tb, g1["token"])
check("creator is the owner", s == 200 and dig(js, "data", "teachers", 0, "isOwner") is True and len(dig(js, "data", "teachers")) == 1, f"{s} {js}")
g1_id = dig(js, "data", "teachers", 0, "id")
s, prof = call("GET", "/guru/profile", g1["token"])
teacher_user_id = dig(js, "data", "teachers", 0, "userId")
profile_user_id = dig(prof, "data", "id")
check("teacher list marks the signed in teacher", s == 200 and dig(js, "data", "teachers", 0, "isSelf") is True and teacher_user_id is not None and teacher_user_id == profile_user_id, f"{s} {js} {prof}")
s, js = call("POST", tb, g1["token"], {"email": g2["email"]})
g2_id = dig(js, "data", "id")
check("add teacher", s == 201 and g2_id and dig(js, "data", "isOwner") is False and dig(js, "data", "isSelf") is False, f"{s} {js}")
expect("add teacher twice", call("POST", tb, g1["token"], {"email": g2["email"]}), 409, "TEACHER_ALREADY_IN_CLASS")
expect("add unknown teacher", call("POST", tb, g1["token"], {"email": f"nobody-{RUN}@example.com"}), 404, "TEACHER_NOT_FOUND")
expect("add a parent as teacher", call("POST", tb, g1["token"], {"email": w1["email"]}), 404, "TEACHER_NOT_FOUND")
expect("non owner cannot remove others", call("DELETE", f"{tb}/{g1_id}", g2["token"]), 403, "CLASS_OWNER_REQUIRED")
expect("non owner cannot hand over", call("PUT", f"/guru/classes/{cid}/owner", g2["token"], {"teacherId": g2_id}), 403, "CLASS_OWNER_REQUIRED")
expect("remove unknown teacher", call("DELETE", f"{tb}/{uuid.uuid4()}", g1["token"]), 404, "TEACHER_NOT_IN_CLASS")
expect("remove bad id", call("DELETE", f"{tb}/nope", g1["token"]), 400, "VALIDATION_ERROR")
expect("hand over to stranger", call("PUT", f"/guru/classes/{cid}/owner", g1["token"], {"teacherId": uuid.uuid4().__str__()}), 404, "TEACHER_NOT_IN_CLASS")
expect("hand over validation", call("PUT", f"/guru/classes/{cid}/owner", g1["token"], {}), 400, "VALIDATION_ERROR")
s, js = call("PUT", f"/guru/classes/{cid}/owner", g1["token"], {"teacherId": g2_id})
check("hand over class", s == 200 and dig(js, "data", "id") == g2_id and dig(js, "data", "isOwner") is True, f"{s} {js}")
expect("old owner cannot hand over again", call("PUT", f"/guru/classes/{cid}/owner", g1["token"], {"teacherId": g1_id}), 403, "CLASS_OWNER_REQUIRED")
expect("naming the owner again is fine", call("PUT", f"/guru/classes/{cid}/owner", g2["token"], {"teacherId": g2_id}), 200)
expect("new owner hands back", call("PUT", f"/guru/classes/{cid}/owner", g2["token"], {"teacherId": g1_id}), 200)
expect("a teacher leaves", call("DELETE", f"{tb}/{g2_id}", g2["token"]), 200)
expect("left teacher no longer sees class", call("GET", tb, g2["token"]), 404, "CLASS_NOT_FOUND")
check("teacher count after leaving", dig(call("GET", tb, g1["token"])[1], "data", "teachers") is not None and len(dig(call("GET", tb, g1["token"])[1], "data", "teachers")) == 1)
expect("only teacher cannot leave", call("DELETE", f"{tb}/{g1_id}", g1["token"]), 409, "LAST_TEACHER")
s, js = call("POST", tb, g1["token"], {"email": g2["email"]})
g2_id = dig(js, "data", "id")
# since 0.13.0 removing or losing a teacher replaces the join code of the class
s, js = call("GET", f"/guru/classes/{cid}", g1["token"])
code_before_removal = dig(js, "data", "joinCode")
expect("owner removes a teacher", call("DELETE", f"{tb}/{g2_id}", g1["token"]), 200)
s, js = call("GET", f"/guru/classes/{cid}", g1["token"])
code_after_removal = dig(js, "data", "joinCode")
check("removing a teacher replaces the join code", code_before_removal and code_after_removal and code_after_removal != code_before_removal and len(code_after_removal) == 8, f"{code_before_removal} {code_after_removal}")
s, js = call("POST", tb, g1["token"], {"email": g2["email"]})
g2_id = dig(js, "data", "id")
expect("owner leaves", call("DELETE", f"{tb}/{g1_id}", g1["token"]), 200)
s, js = call("GET", tb, g2["token"])
check("ownership moved to the remaining teacher", s == 200 and len(dig(js, "data", "teachers")) == 1 and dig(js, "data", "teachers", 0, "isOwner") is True, f"{s} {js}")
expect("former owner is out", call("GET", tb, g1["token"]), 404, "CLASS_NOT_FOUND")
s, js = call("POST", tb, g2["token"], {"email": g1["email"]})
check("new owner re-adds the former one", s == 201, f"{s} {js}")
# the codes above were replaced, so later checks read the current one
s, js = call("GET", f"/guru/classes/{cid}", g2["token"])
new_code = dig(js, "data", "joinCode")
g1["code"] = new_code

# ---------- 0.11.0 idempotency keys, recordedAt and changing the password ----------
key = f"smoke-{RUN}-keyed"
mood = {"studentId": w1["student"], "moodType": "SENANG"}
s1, j1, r1 = call_key("POST", "/guru/mood/log", g1["token"], key, mood)
check("keyed mood log is created", s1 == 201 and not r1, f"{s1} {j1}")
s2, j2, r2 = call_key("POST", "/guru/mood/log", g1["token"], key, mood)
check("a repeat with the same key is replayed", s2 == 201 and r2 and dig(j2, "data", "logId") == dig(j1, "data", "logId"), f"{s2} {r2} {j2}")
s3, j3, _ = call_key("POST", "/guru/mood/log", g1["token"], key, {"studentId": w1["student"], "moodType": "MARAH"})
check("the same key for another request is refused", s3 == 422 and j3.get("errorCode") == "IDEMPOTENCY_KEY_REUSED", f"{s3} {j3}")
s4, j4, _ = call_key("POST", "/guru/mood/log", g1["token"], "short", mood)
check("a bad key is refused", s4 == 400 and j4.get("errorCode") == "VALIDATION_ERROR", f"{s4} {j4}")
s5, j5, _ = call_key("POST", "/classes/" + g1["class"] + "/forum", g1["token"], f"smoke-{RUN}-thread", {"title": "Keyed", "body": "Keyed thread"})
s6, j6, r6 = call_key("POST", "/classes/" + g1["class"] + "/forum", g1["token"], f"smoke-{RUN}-thread", {"title": "Keyed", "body": "Keyed thread"})
check("a keyed forum thread is created once", s5 == 201 and s6 == 201 and r6 and dig(j5, "data", "id") == dig(j6, "data", "id"), f"{s5} {s6} {r6}")

def same_time(got, want):
    try:
        return datetime.fromisoformat(got.replace("Z", "+00:00")) == datetime.fromisoformat(want.replace("Z", "+00:00"))
    except (AttributeError, ValueError):
        return False

def iso(offset):
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + offset))

hour, month = 3600, 30 * 86400
sent = iso(-hour)
s, js = call("POST", "/guru/mood/log", g1["token"], {**mood, "recordedAt": sent})
check("recordedAt in the past is kept", s == 201 and same_time(dig(js, "data", "recordedAt"), sent), f"{s} {js}")
expect("recordedAt in the future is refused", call("POST", "/guru/mood/log", g1["token"], {**mood, "recordedAt": iso(2 * hour)}), 400, "VALIDATION_ERROR")
expect("recordedAt older than 7 days is refused", call("POST", "/guru/mood/log", g1["token"], {**mood, "recordedAt": iso(-month)}), 400, "VALIDATION_ERROR")
expect("recordedAt that is no time is refused", call("POST", "/guru/mood/log", g1["token"], {**mood, "recordedAt": "yesterday"}), 400, "VALIDATION_ERROR")

w5 = register_wali("w5", new_code, "Smoke Child Five")
new_pw = "Smoke-" + secrets.token_urlsafe(18) + "2!"
expect("wrong current password", call("PUT", "/users/me/password", w5["token"], {"currentPassword": "wrong-password", "newPassword": new_pw}), 403, "AUTH_PASSWORD_INCORRECT")
expect("same password", call("PUT", "/users/me/password", w5["token"], {"currentPassword": PW, "newPassword": PW}), 400, "VALIDATION_ERROR")
expect("short new password", call("PUT", "/users/me/password", w5["token"], {"currentPassword": PW, "newPassword": "short"}), 400, "VALIDATION_ERROR")
expect("password change needs a token", call("PUT", "/users/me/password", None, {"currentPassword": PW, "newPassword": new_pw}), 401, "AUTH_TOKEN_MISSING")
expect("a parent changes the password", call("PUT", "/users/me/password", w5["token"], {"currentPassword": PW, "newPassword": new_pw}), 200)
w5["pw"] = new_pw
expect("old password no longer signs in", call("POST", "/auth/login", body={"email": w5["email"], "password": PW, "role": "WALI"}), 401, "AUTH_INVALID_CREDENTIALS")
expect("new password signs in", call("POST", "/auth/login", body={"email": w5["email"], "password": new_pw, "role": "WALI"}), 200)
expect("refresh token ended with the change", call("POST", "/auth/refresh-token", body={"refreshToken": w5["refresh"]}), 401, "AUTH_REFRESH_INVALID")

# ---------- 0.13.0 daily limit of mood logs ----------
limit_hit = None
for attempt in range(1, 26):
    s, js = call("POST", "/guru/mood/log", g1["token"], {"studentId": w2["student"], "moodType": "SENANG"})
    if s != 201:
        limit_hit = (attempt, s, js.get("errorCode"))
        break
check("a child stops taking mood logs after 20 in a day", limit_hit is not None and limit_hit[0] <= 21 and limit_hit[1:] == (409, "MOOD_LOG_LIMIT_REACHED"), str(limit_hit))
s, js = call("POST", "/guru/mood/log/batch", g1["token"], {"classId": g1["class"], "entries": [
    {"studentId": w2["student"], "moodType": "SENANG", "source": "MANUAL_INPUT"}]})
check("a batch with a full child is refused and names the entry", s == 409 and js.get("errorCode") == "MOOD_LOG_LIMIT_REACHED" and "entries[0].studentId" in (js.get("details") or {}), f"{s} {js}")
expect("mood notes over 500 characters are refused", call("POST", "/guru/mood/log", g1["token"], {"studentId": w1["student"], "moodType": "SENANG", "notes": "a" * 501}), 400, "VALIDATION_ERROR")

# ---------- 0.9.0 deleting accounts ----------
expect("parent wrong password", call("DELETE", "/users/me", w2["token"], {"password": "wrong-password"}), 403, "AUTH_PASSWORD_INCORRECT")
expect("parent delete validation", call("DELETE", "/users/me", w2["token"], {}), 400, "VALIDATION_ERROR")
expect("parent deletes account", delete_me(w2), 200)
expect("deleted parent locked out", call("GET", "/wali/dashboard", w2["token"]), 404, "PROFILE_NOT_FOUND")
expect("deleted parent cannot login", call("POST", "/auth/login", body={"email": w2["email"], "password": PW, "role": "WALI"}), 401, "AUTH_INVALID_CREDENTIALS")

# g3 is the only teacher of its class. A class without children is archived instead of needing a successor since 0.11.0,
# so it gets a child first, and g1's second class gets one for the same reason.
register_wali("w3", g3["code"], "Smoke Child Of G3")
check("g1's second class has a child", register_wali("w6", g1_second_code, "Smoke Child Of A2")["student"] is not None)
expect("sole teacher needs a successor", delete_me(g3), 409, "SUCCESSOR_REQUIRED")
expect("empty successor is the same", delete_me(g3, successorEmail=""), 409, "SUCCESSOR_REQUIRED")
expect("unknown successor", delete_me(g3, successorEmail=f"nobody-{RUN}@example.com"), 404, "TEACHER_NOT_FOUND")
expect("parent as successor", delete_me(g3, successorEmail=w1["email"]), 404, "TEACHER_NOT_FOUND")
expect("self as successor", delete_me(g3, successorEmail=g3["email"]), 400, "VALIDATION_ERROR")
expect("bad successor email", delete_me(g3, successorEmail="not-an-email"), 400, "VALIDATION_ERROR")
check("refused deletions changed nothing", call("GET", "/guru/dashboard", g3["token"])[0] == 200)
s, js = call("GET", f"/guru/classes/{g3['class']}/teachers", g3["token"])
check("g3 still owns its class", s == 200 and dig(js, "data", "teachers", 0, "isOwner") is True)
expect("successor takes the class (case ignored)", delete_me(g3, successorEmail=g4["email"].upper()), 200)
expect("deleted teacher locked out", call("GET", "/guru/dashboard", g3["token"]), 404, "PROFILE_NOT_FOUND")
s, js = call("GET", f"/guru/classes/{g3['class']}/teachers", g4["token"])
check("successor owns the class", s == 200 and len(dig(js, "data", "teachers")) == 1 and dig(js, "data", "teachers", 0, "isOwner") is True, f"{s} {js}")
check("class keeps taking children", register_wali("w3b", g3["code"], "Smoke Child Late")["student"] is not None)

# g1 and g2 share the first class, g1 also owns a second class that nobody else teaches
expect("shared class alone is not enough", delete_me(g1, successorEmail=""), 409, "SUCCESSOR_REQUIRED")
s, js = call("GET", tb, g2["token"])
owner_before = [t["id"] for t in dig(js, "data", "teachers") if t.get("isOwner")]
check("refusal rolled back the shared class", len(dig(js, "data", "teachers")) == 2 and owner_before == [g2_id], f"{owner_before} {g2_id}")
expect("g1 deletes with successor", delete_me(g1, successorEmail=g4["email"]), 200)
s, js = call("GET", tb, g2["token"])
check("shared class stays with g2", s == 200 and len(dig(js, "data", "teachers")) == 1 and dig(js, "data", "teachers", 0, "isOwner") is True, f"{s} {js}")
s, js = call("GET", f"/guru/classes/{g1_second}/teachers", g4["token"])
check("g1's second class went to the successor", s == 200 and len(dig(js, "data", "teachers")) == 1 and dig(js, "data", "teachers", 0, "isOwner") is True, f"{s} {js}")

# parents of a deleted teacher's class still work
expect("parent still reads the class", call("GET", "/wali/dashboard", w1["token"]), 200)
expect("parent deletes account", delete_me(w1), 200)

# g2 now has the shared class, its own class and nothing else
expect("g2 deletes with successor", delete_me(g2, successorEmail=g4["email"]), 200)
s, js = call("GET", "/guru/classes", g4["token"])
classes = dig(js, "data", "classes") or []
check("successor teaches every handed class", s == 200 and dig(js, "data", "total") == keeper_before + 4, f"{s} total={dig(js, 'data', 'total')} before={keeper_before}")
okowner = True
for c in classes:
    s2, j2 = call("GET", f"/guru/classes/{c['id']}/teachers", g4["token"])
    ts = dig(j2, "data", "teachers") or []
    okowner = okowner and len(ts) == 1 and ts[0].get("isOwner") is True
check("successor owns each of them", okowner)
# This is the one request that would delete the keeper if the rule were broken, so it only runs while every earlier check
# has passed. After a failure the state of the keeper's classes is not known, and the run already fails.
if not failed:
    expect("the last teacher cannot delete without another", delete_me(g4), 409, "SUCCESSOR_REQUIRED")

# a class without children is archived when its only teacher deletes the account without a successor
g5 = register_guru("g5", "Smoke D")
expect("a teacher with an empty class deletes without a successor", delete_me(g5), 200)
expect("the join code of an archived class is not found", call("POST", "/auth/register", body={
    "email": f"kibooz-smoke-{RUN}-late@example.com", "password": PW, "fullName": "Late Parent", "role": "WALI",
    "classCode": g5["code"], "student": {"nisn": nisn(), "fullName": "Smoke Late Child"}}), 404, "CLASS_CODE_NOT_FOUND")


# ---------- 0.13.0 request size ----------
big = json.dumps({"email": "kibooz-smoke-size@example.com", "password": "x" * 70000}).encode()
expect("a JSON body over 64 KB is refused", call("POST", "/auth/login", raw=big, ctype="application/json"), 413, "BODY_TOO_LARGE")


sys.exit(1 if failed else 0)
