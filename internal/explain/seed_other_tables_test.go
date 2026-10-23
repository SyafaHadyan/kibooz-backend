//go:build explain

package explain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedOtherTables fills the tables that the first seed leaves empty, so that the queries on moods, scans, the forum, videos, the
// leaderboard history, sessions, push tokens, the file cleanup queue and idempotency keys run against rows and not against an
// empty table, where every plan would look cheap.
func seedOtherTables(t *testing.T, database *gorm.DB, classes int, children int) {
	t.Helper()

	steps := []struct {
		name string
		sql  string
		args []any
	}{
		// the moods and the reports only count what was written after the child joined the class, so the joins are moved back
		{"enrollments activated earlier", `UPDATE enrollments SET activated_at = now() - interval '400 days' WHERE status = 'active'`, nil},
		{"moods", `INSERT INTO mood_logs (student_id, recorded_by_guru_id, mood_type, confidence_score, source, recorded_at)
			SELECT md5('s' || i)::uuid, md5('g' || (1 + i % ?::int))::uuid, (ARRAY['SENANG','SEDIH','MARAH','BINGUNG'])[1 + (i + d) % 4]::mood_enum,
				0.9, 'MANUAL_INPUT', now() - make_interval(days => d + 1)
			FROM generate_series(1, ?::int) AS i, generate_series(0, 2) AS d`, []any{classes, children}},
		{"trash scans", `INSERT INTO trash_scans (student_id, trash_type, confidence_score, points_awarded, scanned_at)
			SELECT md5('s' || i)::uuid, (ARRAY['ORGANIK','ANORGANIK','B3'])[1 + (i + d) % 3]::trash_category_enum, 0.9, 5,
				now() - make_interval(hours => d * 7 + i % 100)
			FROM generate_series(1, ?::int) AS i, generate_series(0, 4) AS d`, []any{children}},
		{"forum threads", `INSERT INTO forum_posts (id, class_id, parent_id, author_user_id, title, body, created_at)
			SELECT md5('thread' || c || '-' || t)::uuid, md5('class' || c)::uuid, NULL, md5('guru' || c)::uuid, 'Thread ' || t, 'Body ' || t,
				now() - make_interval(mins => t)
			FROM generate_series(1, ?::int) AS c, generate_series(1, 20) AS t`, []any{classes}},
		{"forum replies", `INSERT INTO forum_posts (id, class_id, parent_id, author_user_id, title, body, created_at)
			SELECT md5('reply' || c || '-' || t || '-' || r)::uuid, md5('class' || c)::uuid, md5('thread' || c || '-' || t)::uuid,
				md5('wali' || c)::uuid, NULL, 'Reply ' || r, now() - make_interval(secs => r)
			FROM generate_series(1, ?::int) AS c, generate_series(1, 20) AS t, generate_series(1, 5) AS r`, []any{classes}},
		{"videos", `INSERT INTO learning_videos (class_id, added_by_guru_id, title, video_url, created_at)
			SELECT md5('class' || c)::uuid, md5('g' || c)::uuid, 'Video ' || v, 'https://files.example.test/videos/' || c || '-' || v || '.mp4',
				now() - make_interval(mins => v)
			FROM generate_series(1, ?::int) AS c, generate_series(1, 10) AS v`, []any{classes}},
		{"leaderboard seasons", `INSERT INTO leaderboard_seasons (label, started_at) VALUES
			('2025-07', now() - interval '16 months'), ('2026-01', now() - interval '10 months') ON CONFLICT DO NOTHING`, nil},
		{"leaderboard history", `INSERT INTO leaderboard_history (class_id, student_id, student_name, season_label, points, rank_position)
			SELECT md5('class' || (1 + i % ?::int))::uuid, md5('s' || i)::uuid, 'Child ' || i, l, (i * 7) % 500, 1 + i / ?::int
			FROM generate_series(1, ?::int) AS i, unnest(ARRAY['2025-07', '2026-01']) AS l`, []any{classes, classes, children}},
		// one in five sessions has expired and was used, which is what the sweeper deletes
		{"sessions", `INSERT INTO refresh_tokens (user_id, token_hash, expires_at, family_id, session_started_at, used_at)
			SELECT md5('wali' || i)::uuid, md5('t' || i) || md5('u' || i),
				CASE WHEN i % 5 = 0 THEN now() - interval '30 days' ELSE now() + interval '6 days' END, gen_random_uuid(),
				CASE WHEN i % 5 = 0 THEN now() - interval '100 days' ELSE now() - interval '1 day' END,
				CASE WHEN i % 5 = 0 THEN now() - interval '31 days' ELSE NULL END
			FROM generate_series(1, ?::int) AS i`, []any{children}},
		{"push tokens", `INSERT INTO push_tokens (user_id, token) SELECT md5('wali' || i)::uuid, md5('p' || i) || md5('q' || i)
			FROM generate_series(1, ?::int) AS i`, []any{children}},
		{"reads of the other person", `INSERT INTO chat_reads (conversation_id, user_id, last_read_message_id)
			SELECT c.id, c.user_high_id, 0 FROM chat_conversations AS c ON CONFLICT DO NOTHING`, nil},
		// one in five is due, the rest wait for a day
		{"file cleanups", `INSERT INTO file_cleanups (object_key, next_try_at)
			SELECT 'old/' || i || '.jpg', CASE WHEN i % 5 = 0 THEN now() - interval '1 hour' ELSE now() + interval '1 day' END
			FROM generate_series(1, 20000) AS i`, nil},
		{"idempotency keys", `INSERT INTO idempotency_keys (user_id, idempotency_key, fingerprint, status_code, response, created_at)
			SELECT md5('wali' || i)::uuid, md5('k' || i), decode(md5('f' || i), 'hex'), 200, '\x7b7d'::bytea, now() - make_interval(hours => i % 72)
			FROM generate_series(1, ?::int) AS i`, []any{children}},
	}

	for _, step := range steps {
		started := time.Now()

		require.NoError(t, database.Exec(step.sql, step.args...).Error, step.name)
		t.Logf("seeded %s in %s", step.name, time.Since(started).Round(time.Millisecond))
	}

	require.NoError(t, database.Exec("ANALYZE").Error)
}
