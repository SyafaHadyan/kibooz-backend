//go:build explain

// Package explain seeds a throwaway PostgreSQL with about 200000 accounts and prints the plans of the queries that the scale
// review marked as the ones to watch. It only runs with the build tag explain, against the database that DB_HOST, DB_PORT, DB_NAME,
// DB_USERNAME and DB_PASSWORD name, which has to be one that nobody needs, because the test applies the migrations and fills it. The
// Explain plans workflow does exactly that on a runner and keeps the result, see docs/testing.md. Never point it at a real database.
package explain

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	chatrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/chat/repository"
	classroomrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/db"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

// the size of the seed, which the workflow can lower with EXPLAIN_CLASSES for a quick try
const (
	defaultClasses   = 4000
	childrenPerClass = 49
)

// the time that one query of a section and one EXPLAIN may take. A query that needs more than this at the size of the seed is the
// finding, and the run goes on to the next section with what it has, so one slow plan cannot cost the others or the job.
const (
	queryTimeout   = 3 * time.Minute
	planTimeout    = 3 * time.Minute
	costOnlyBudget = time.Minute
	seasonLabel    = "2099-01-01"
)

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}

	return value
}

func config() *env.Env {
	port, _ := strconv.Atoi(os.Getenv("DB_PORT"))

	return &env.Env{
		DBHost: os.Getenv("DB_HOST"), DBPort: uint(port), DBName: os.Getenv("DB_NAME"),
		DBUsername: os.Getenv("DB_USERNAME"), DBPassword: os.Getenv("DB_PASSWORD"), DBSSLMode: "disable",
	}
}

// seed fills the database with classes of 49 children, a teacher for each class, a direct conversation between the teacher and the
// parent of every second child with messages in it, and guidance applications. The ids come from md5 so that a query can name them.
func seed(t *testing.T, database *gorm.DB, classes int) int {
	t.Helper()

	children := classes * childrenPerClass
	conversations := children / 2

	steps := []struct {
		name string
		sql  string
		args []any
	}{
		{"classes", `INSERT INTO classes (id, join_code, name)
			SELECT md5('class' || i)::uuid, lpad(to_hex(i), 8, '0'), 'Class ' || i FROM generate_series(1, ?::int) AS i`, []any{classes}},
		{"teacher users", `INSERT INTO users (id, email, password_hash, role, full_name)
			SELECT md5('guru' || i)::uuid, 'guru' || i || '@example.test', 'x', 'GURU', 'Guru ' || i FROM generate_series(1, ?::int) AS i`, []any{classes}},
		{"parent users", `INSERT INTO users (id, email, password_hash, role, full_name)
			SELECT md5('wali' || i)::uuid, 'wali' || i || '@example.test', 'x', 'WALI', 'Wali ' || i FROM generate_series(1, ?::int) AS i`, []any{children}},
		{"gurus", `INSERT INTO gurus (id, user_id) SELECT md5('g' || i)::uuid, md5('guru' || i)::uuid FROM generate_series(1, ?::int) AS i`, []any{classes}},
		{"walis", `INSERT INTO walis (id, user_id) SELECT md5('w' || i)::uuid, md5('wali' || i)::uuid FROM generate_series(1, ?::int) AS i`, []any{children}},
		{"class teachers", `INSERT INTO class_teachers (class_id, guru_id, is_owner)
			SELECT md5('class' || i)::uuid, md5('g' || i)::uuid, true FROM generate_series(1, ?::int) AS i`, []any{classes}},
		{"children", `INSERT INTO students (id, wali_id, class_id, nisn, full_name, current_points, season_points)
			SELECT md5('s' || i)::uuid, md5('w' || i)::uuid, md5('class' || (1 + i % ?::int))::uuid, lpad(i::text, 10, '0'), 'Child ' || i,
				(i * 7) % 500, (i * 3) % 200 FROM generate_series(1, ?::int) AS i`, []any{classes, children}},
		{"conversations", `INSERT INTO chat_conversations (id, user_low_id, user_high_id, kind)
			SELECT md5('conv' || i)::uuid, LEAST(md5('guru' || (1 + i % ?::int))::uuid, md5('wali' || i)::uuid),
				GREATEST(md5('guru' || (1 + i % ?::int))::uuid, md5('wali' || i)::uuid), 'direct'
			FROM generate_series(1, ?::int) AS i`, []any{classes, classes, conversations}},
		{"messages", `INSERT INTO chat_messages (conversation_id, sender_id, body, created_at)
			SELECT md5('conv' || (1 + i % ?::int))::uuid, CASE WHEN i % 2 = 0 THEN md5('guru' || (1 + (1 + i % ?::int) % ?::int))::uuid
				ELSE md5('wali' || (1 + i % ?::int))::uuid END,
				'Message ' || i, now() - make_interval(secs => i) FROM generate_series(1, ?::int) AS i`, []any{conversations, conversations, classes, conversations, conversations * 10}},
		{"reads", `INSERT INTO chat_reads (conversation_id, user_id, last_read_message_id)
			SELECT c.id, c.user_low_id, 0 FROM chat_conversations AS c`, nil},
		{"guidance applications", `INSERT INTO guidance_applications (student_id, wali_id, guidance_id, parent_notes, applied_at)
			SELECT md5('s' || (1 + i % ?::int))::uuid, md5('w' || (1 + i % ?::int))::uuid, 'guidance-bingung-4step', 'Note ' || i,
				now() - make_interval(secs => i * 60) FROM generate_series(1, ?::int) AS i`, []any{children, children, children * 2}},
	}

	for _, step := range steps {
		started := time.Now()

		require.NoError(t, database.Exec(step.sql, step.args...).Error, step.name)
		t.Logf("seeded %s in %s", step.name, time.Since(started).Round(time.Millisecond))
	}

	require.NoError(t, database.Exec("ANALYZE").Error)

	return children
}

// planLogger writes the plan of every select that the repositories run, with the values filled in. It first writes the plan without
// running the query, so a query that is too slow to run still leaves its plan, and then the plan with the real timings.
type planLogger struct {
	gormlogger.Interface

	explain *gorm.DB
	plans   io.Writer
}

func (l *planLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }

func (l *planLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), _ error) {
	statement, rows := fc()

	upper := strings.ToUpper(strings.TrimSpace(statement))
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return
	}

	fmt.Fprintf(l.plans, "-- %d rows in %s\n%s\n", rows, time.Since(begin).Round(time.Microsecond), statement)

	// the caller may have used up its own deadline, so the plans run on a context of their own
	l.write(ctx, "EXPLAIN", statement, costOnlyBudget)
	l.write(ctx, "EXPLAIN (ANALYZE, BUFFERS)", statement, planTimeout)
}

func (l *planLogger) write(parent context.Context, command string, statement string, limit time.Duration) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), limit)
	defer cancel()

	var lines []string

	err := l.explain.WithContext(ctx).Raw(command + " " + statement).Scan(&lines).Error
	if err != nil {
		fmt.Fprintf(l.plans, "%s failed %v\n\n", command, err)

		return
	}

	fmt.Fprintf(l.plans, "%s\n\n", strings.Join(lines, "\n"))
}

func TestExplainThePlansOfTheQueriesToWatch(t *testing.T) {
	cfg := config()
	require.NotEmpty(t, cfg.DBName, "DB_NAME names the throwaway database")

	database, err := db.New(cfg)
	require.NoError(t, err)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, db.Migrate(sqlDB))

	classes := envInt("EXPLAIN_CLASSES", defaultClasses)
	children := seed(t, database, classes)
	t.Logf("%d classes, %d children, %d accounts in all", classes, children, classes+children)

	// a second handle without a logger, so the plans of EXPLAIN itself are not explained again
	explainDB, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: gormlogger.Discard})
	require.NoError(t, err)

	out := os.Getenv("EXPLAIN_OUT")
	if out == "" {
		out = "explain-plans.txt"
	}

	// the file is written as the plans come, so a failure or a timeout later keeps the ones that exist
	plans, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	require.NoError(t, err)

	t.Cleanup(func() { _ = plans.Close() })

	logged := database.Session(&gorm.Session{Logger: &planLogger{Interface: gormlogger.Discard, explain: explainDB, plans: plans}})

	// every class carries the label of the semester, which is what the season job finds on every minute but the first one
	require.NoError(t, database.Exec("UPDATE class_seasons SET season_label = ?", seasonLabel).Error)

	ctx := context.Background()
	teacher := uuid.MustParse(fmt.Sprintf("%x", md5sum("guru1")))
	parent := uuid.MustParse(fmt.Sprintf("%x", md5sum("wali1")))
	class := uuid.MustParse(fmt.Sprintf("%x", md5sum("class1")))

	chat := chatrepository.NewChatDB(logged)
	classroom := classroomrepository.NewClassroomDB(logged)

	section := func(title string, run func(ctx context.Context)) {
		fmt.Fprintf(plans, "==== %s\n\n", title)

		ctx, cancel := context.WithTimeout(ctx, queryTimeout)
		defer cancel()

		run(ctx)
	}

	section("chat contacts of a teacher", func(ctx context.Context) {
		_, _, err := chat.ListContacts(ctx, teacher, constants.RoleWali, 20, 0)
		assert.NoError(t, err)
	})
	section("chat conversations of a teacher", func(ctx context.Context) {
		_, _, err := chat.ListConversations(ctx, teacher, 20, 0)
		assert.NoError(t, err)
	})
	section("chat conversations of a parent", func(ctx context.Context) {
		_, _, err := chat.ListConversations(ctx, parent, 20, 0)
		assert.NoError(t, err)
	})
	section("guidance reports of a class", func(ctx context.Context) {
		_, _, err := classroom.ListGuidanceReports(ctx, class, 20, 0)
		assert.NoError(t, err)
	})
	section("season job, every class already in the semester", func(ctx context.Context) {
		// the text of the query of internal/season startClasses, with the label that every class has, so it scans all of them and finds none
		var ids []uuid.UUID

		assert.NoError(t, logged.WithContext(ctx).Raw(
			"SELECT c.id FROM classes AS c LEFT JOIN class_seasons AS s ON s.class_id = c.id "+
				"WHERE s.season_label IS DISTINCT FROM ? AND c.id > ? ORDER BY c.id LIMIT ?",
			seasonLabel, uuid.Nil, 50,
		).Scan(&ids).Error)
	})

	t.Logf("the plans are in %s", out)
}

// md5sum is the hash that the seed uses for its ids, md5 of the text, which Postgres reads as a uuid
func md5sum(text string) [16]byte { return md5.Sum([]byte(text)) }
