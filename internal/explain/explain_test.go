//go:build explain

// Package explain seeds a throwaway PostgreSQL with about 200000 accounts and writes the plans of the queries of every repository
// and of the jobs that run on their own. It only runs with the build tag explain, against the database that DB_HOST, DB_PORT, DB_NAME,
// DB_USERNAME and DB_PASSWORD name, which has to be one that nobody needs, because the test applies the migrations and fills it. The
// Explain plans workflow does exactly that on a runner and keeps the result, see docs/testing.md. Never point it at a real database.
package explain

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	authrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/auth/repository"
	chatrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/chat/repository"
	classroomrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/repository"
	gururepository "github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	pushrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/push/repository"
	trashrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/trash/repository"
	userrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
	walirepository "github.com/SyafaHadyan/kibooz-backend/internal/app/wali/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/enrollment"
	"github.com/SyafaHadyan/kibooz-backend/internal/filecleanup"
	"github.com/SyafaHadyan/kibooz-backend/internal/idempotency"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/db"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
	"github.com/SyafaHadyan/kibooz-backend/internal/nisn"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
	"github.com/SyafaHadyan/kibooz-backend/internal/season"
	"github.com/SyafaHadyan/kibooz-backend/internal/sessions"
)

// the time that one query of a section and one EXPLAIN may take. A query that needs more than this at the size of the seed is the
// finding, and the run goes on to the next section with what it has, so one slow plan cannot cost the others or the job.
const (
	costOnlyBudget = time.Minute
	seasonLabel    = "2099-01-01"
)

// queryTimeout is how long one call may take, and a plan with real timings gets the same time. The manual run keeps the 3
// minutes. The check of a pull request sets EXPLAIN_QUERY_TIMEOUT_SECONDS to a few seconds on a smaller seed, so a query that
// grows with the square of the data fails there before it reaches the full size.
var (
	queryTimeout = time.Duration(envInt("EXPLAIN_QUERY_TIMEOUT_SECONDS", 180)) * time.Second
	planTimeout  = queryTimeout
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

// planLogger writes the plan of every statement that the repositories run, with the values filled in. It first writes the plan
// without running the statement, so a query that is too slow to run still leaves its plan, and then the plan with the real
// timings for a select that can be run again without a side effect.
type planLogger struct {
	gormlogger.Interface

	explain *gorm.DB
	plans   io.Writer
}

func (l *planLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }

func (l *planLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), _ error) {
	statement, rows := fc()

	upper := strings.ToUpper(strings.TrimSpace(statement))
	read := strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH")
	write := strings.HasPrefix(upper, "INSERT") || strings.HasPrefix(upper, "UPDATE") || strings.HasPrefix(upper, "DELETE")

	if !read && !write {
		return
	}

	fmt.Fprintf(l.plans, "-- %d rows in %s\n%s\n", rows, time.Since(begin).Round(time.Microsecond), statement)

	// the plans run on a context of their own, because the caller may have used up its deadline
	l.write(ctx, "EXPLAIN", statement, costOnlyBudget)

	// A statement that changes rows, takes a row lock or waits for an advisory lock is only explained and not run again. It runs
	// on a connection of its own while the transaction of the caller is still open, so running it would wait for that
	// transaction, or change the rows a second time.
	if read && !strings.Contains(upper, " FOR UPDATE") && !strings.Contains(upper, " FOR SHARE") &&
		!strings.Contains(upper, " FOR NO KEY UPDATE") && !strings.Contains(upper, "ADVISORY") && !strings.Contains(upper, "RETURNING") {
		l.write(ctx, "EXPLAIN (ANALYZE, BUFFERS)", statement, planTimeout)
	}
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

// id is the uuid that the seed gives to the row named by the text, the md5 of the text
func id(text string) uuid.UUID { return uuid.UUID(md5.Sum([]byte(text))) }

func drop[T any](_ T, err error) error { return err }

func drop2[A any, B any](_ A, _ B, err error) error { return err }

func drop3[A any, B any, C any](_ A, _ B, _ C, err error) error { return err }

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

	runCalls(t, logged, plans, classes, children)

	t.Logf("the plans are in %s", out)
}

// runCalls calls every repository method that reads, and the ones that write through a statement worth a plan, with the ids of
// one class of the seed. The class is the second one, because the child of the first parent and the first conversation belong to
// it, so a teacher, a parent, a child and a conversation are linked.
func runCalls(t *testing.T, logged *gorm.DB, plans io.Writer, classes int, children int) {
	t.Helper()

	var (
		teacher   = id("guru2")
		guru      = id("g2")
		class     = id("class2")
		parent    = id("wali1")
		wali      = id("w1")
		child     = id("s1")
		secondTwo = id("s" + strconv.Itoa(1+classes))
		chatID    = id("conv1")
		thread    = id("thread2-1")
		reply     = id("reply2-1-1")
		now       = time.Now()
		from      = now.Add(-30 * 24 * time.Hour)
		// the time that the account joined the class, because the zero time is not a date that PostgreSQL accepts in a written out statement
		joined = now.AddDate(-1, 0, 0)
		to     = now
		utc    = time.UTC
		keyOf  = func(string) (string, bool) { return "", false }
	)

	var (
		video     uuid.UUID
		messageID int64
		session   = fmt.Sprintf("%x%x", md5.Sum([]byte("t1")), md5.Sum([]byte("u1")))
		spare     = fmt.Sprintf("%x%x", md5.Sum([]byte("t2")), md5.Sum([]byte("u2")))
	)

	// Row and Scan, because a uuid.UUID is an array of bytes that the plain scan of GORM cannot fill from the text of a uuid
	quiet := logged.Session(&gorm.Session{Logger: gormlogger.Discard})

	require.NoError(t, quiet.Raw("SELECT id FROM learning_videos WHERE class_id = ? LIMIT 1", class).Row().Scan(&video))
	require.NoError(t, quiet.Raw("SELECT MIN(id) FROM chat_messages WHERE conversation_id = ?", chatID).Row().Scan(&messageID))

	auth := authrepository.NewAuthDB(logged)
	chat := chatrepository.NewChatDB(logged)
	classroom := classroomrepository.NewClassroomDB(logged)
	guruRepo := gururepository.NewGuruDB(logged, keyOf)
	pushRepo := pushrepository.NewPushDB(logged)
	trash := trashrepository.NewTrashDB(logged)
	userRepo := userrepository.NewUserDB(logged)
	waliRepo := walirepository.NewWaliDB(logged, keyOf)

	call := func(title string, run func(ctx context.Context) error) {
		fmt.Fprintf(plans, "==== %s\n\n", title)

		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		defer cancel()

		err := run(ctx)

		switch {
		case err == nil:
		case errors.Is(err, context.DeadlineExceeded):
			fmt.Fprintf(plans, "!! ran out of time after %s\n\n", queryTimeout)
			t.Errorf("%s ran out of time after %s", title, queryTimeout)
		default:
			// a not found or a refusal is an answer of the code, and the plans of its statements are above
			fmt.Fprintf(plans, "returned %v\n\n", err)
		}
	}

	// reading
	call("auth, find a user by email", func(ctx context.Context) error { return drop(auth.FindUserByEmail(ctx, "guru2@example.test")) })
	call("auth, find a user by id", func(ctx context.Context) error { return drop(auth.FindUserByID(ctx, teacher)) })
	call("auth, class code exists", func(ctx context.Context) error { return drop(auth.ClassCodeExists(ctx, "00000002")) })
	call("auth, registration taken", func(ctx context.Context) error {
		return auth.RegistrationTaken(ctx, "new@example.test", "1999000", "9999999999")
	})
	call("chat, find an account", func(ctx context.Context) error { return drop(chat.FindAccount(ctx, parent)) })
	call("chat, share a class", func(ctx context.Context) error { return drop(chat.ShareClass(ctx, teacher, parent)) })
	call("chat, contacts of a teacher", func(ctx context.Context) error {
		return drop2(chat.ListContacts(ctx, teacher, constants.RoleWali, 20, 0))
	})
	call("chat, contacts of a parent", func(ctx context.Context) error {
		return drop2(chat.ListContacts(ctx, parent, constants.RoleGuru, 20, 0))
	})
	call("chat, contact links", func(ctx context.Context) error { return drop(chat.ContactLinks(ctx, teacher, []uuid.UUID{parent})) })
	call("chat, conversation links", func(ctx context.Context) error {
		return drop(chat.ConversationLinks(ctx, teacher, []uuid.UUID{parent}))
	})
	call("chat, member since", func(ctx context.Context) error { return drop(chat.MemberSince(ctx, parent, class)) })
	call("chat, class member ids", func(ctx context.Context) error { return drop(chat.ClassMemberIDs(ctx, class)) })
	call("chat, class members joined by", func(ctx context.Context) error { return drop(chat.ClassMembersJoinedBy(ctx, class, now)) })
	call("chat, find the room of a class", func(ctx context.Context) error { return drop(chat.FindClassRoom(ctx, class)) })
	call("chat, find a conversation", func(ctx context.Context) error { return drop(chat.FindConversation(ctx, chatID)) })
	call("chat, find a conversation for a parent", func(ctx context.Context) error {
		return drop(chat.FindConversationFor(ctx, parent, chatID))
	})
	call("chat, conversations of a teacher", func(ctx context.Context) error { return drop2(chat.ListConversations(ctx, teacher, 20, 0)) })
	call("chat, conversations of a parent", func(ctx context.Context) error { return drop2(chat.ListConversations(ctx, parent, 20, 0)) })
	call("chat, latest messages", func(ctx context.Context) error {
		return drop2(chat.ListMessages(ctx, chatID, joined, nil, nil, 50))
	})
	call("chat, messages after one", func(ctx context.Context) error {
		return drop2(chat.ListMessages(ctx, chatID, joined, &messageID, nil, 50))
	})
	call("chat, find a message", func(ctx context.Context) error { return drop(chat.FindMessage(ctx, chatID, messageID, joined)) })
	call("classroom, teacher of a class", func(ctx context.Context) error { return drop(classroom.TeacherOfClass(ctx, teacher, class)) })
	call("classroom, parent in a class", func(ctx context.Context) error { return drop(classroom.ParentInClass(ctx, parent, class)) })
	call("classroom, videos", func(ctx context.Context) error { return drop2(classroom.ListVideos(ctx, class, 20, 0)) })
	call("classroom, find a video", func(ctx context.Context) error { return drop(classroom.FindVideo(ctx, class, video)) })
	call("classroom, count videos by address", func(ctx context.Context) error {
		return drop(classroom.CountVideosByURL(ctx, "https://files.example.test/videos/2-1.mp4"))
	})
	call("classroom, threads", func(ctx context.Context) error { return drop2(classroom.ListThreads(ctx, class, 20, 0)) })
	call("classroom, thread exists", func(ctx context.Context) error { return drop(classroom.ThreadExists(ctx, class, thread)) })
	call("classroom, class archived", func(ctx context.Context) error { return drop(classroom.ClassArchived(ctx, class)) })
	call("classroom, replies", func(ctx context.Context) error { return drop2(classroom.ListReplies(ctx, thread, 20, 0)) })
	call("classroom, find a post", func(ctx context.Context) error { return drop(classroom.FindPost(ctx, reply)) })
	call("classroom, roster", func(ctx context.Context) error { return drop2(classroom.ListStudents(ctx, class, from, to, 20, 0)) })
	call("classroom, guidance reports", func(ctx context.Context) error { return drop2(classroom.ListGuidanceReports(ctx, class, 20, 0)) })
	call("guru, move requests", func(ctx context.Context) error { return drop(guruRepo.ListMoveRequests(ctx, class)) })
	call("guru, history of a child", func(ctx context.Context) error { return drop(guruRepo.StudentHistory(ctx, class, child)) })
	call("nisn, verified holder", func(ctx context.Context) error {
		return drop(nisn.VerifiedHolder(logged.WithContext(ctx), "1000000001", child))
	})
	call("guru, find a teacher", func(ctx context.Context) error { return drop(guruRepo.FindGuruByUserID(ctx, teacher)) })
	call("guru, find the taught class", func(ctx context.Context) error { return drop(guruRepo.FindTaughtClass(ctx, guru, &class)) })
	call("guru, find the first taught class", func(ctx context.Context) error { return drop(guruRepo.FindTaughtClass(ctx, guru, nil)) })
	call("guru, count students", func(ctx context.Context) error { return drop(guruRepo.CountStudents(ctx, class)) })
	call("guru, mood records", func(ctx context.Context) error { return drop(guruRepo.ListMoodRecords(ctx, class, from, to, utc)) })
	call("guru, find a student", func(ctx context.Context) error { return drop(guruRepo.FindStudent(ctx, child)) })
	call("guru, teaches a class", func(ctx context.Context) error { return drop(guruRepo.TeachesClass(ctx, guru, class)) })
	call("guru, taught classes", func(ctx context.Context) error { return drop2(guruRepo.ListTaughtClasses(ctx, guru, 20, 0)) })
	call("guru, a class row", func(ctx context.Context) error { return drop(guruRepo.FindClassRow(ctx, guru, class)) })
	call("guru, count the content of a class", func(ctx context.Context) error { return drop2(guruRepo.CountClassContent(ctx, class)) })
	call("guru, teachers of a class", func(ctx context.Context) error { return drop(guruRepo.ListTeachers(ctx, class)) })
	call("push, tokens of the people of a conversation", func(ctx context.Context) error {
		return drop(pushRepo.TokensOf(ctx, []uuid.UUID{teacher, parent, id("wali2"), id("wali3")}))
	})
	call("trash, find the parent id", func(ctx context.Context) error { return drop(trash.FindWaliIDByUserID(ctx, parent)) })
	call("trash, find the teacher id", func(ctx context.Context) error { return drop(trash.FindGuruIDByUserID(ctx, teacher)) })
	call("trash, find an owned student", func(ctx context.Context) error { return drop(trash.FindOwnedStudent(ctx, wali, child)) })
	call("trash, count scans", func(ctx context.Context) error { return drop(trash.CountScans(ctx, child, from, to)) })
	call("trash, class students", func(ctx context.Context) error { return drop(trash.ListClassStudents(ctx, class)) })
	call("trash, class students between", func(ctx context.Context) error {
		return drop(trash.ListClassStudentsBetween(ctx, class, from, to))
	})
	call("trash, history seasons", func(ctx context.Context) error { return drop2(trash.ListHistorySeasons(ctx, class, 20, 0)) })
	call("trash, history entries", func(ctx context.Context) error {
		return drop(trash.ListHistoryEntries(ctx, class, []string{"2025-07", "2026-01"}))
	})
	call("trash, first class of a parent", func(ctx context.Context) error { return drop(trash.FirstWaliClass(ctx, wali)) })
	call("trash, parent has a student in a class", func(ctx context.Context) error { return drop(trash.WaliHasStudentInClass(ctx, wali, class)) })
	call("trash, first class of a teacher", func(ctx context.Context) error { return drop(trash.FirstGuruClass(ctx, guru)) })
	call("trash, teacher teaches a class", func(ctx context.Context) error { return drop(trash.GuruTeachesClass(ctx, guru, class)) })
	call("user, find a user", func(ctx context.Context) error { return drop(userRepo.FindUserByID(ctx, parent)) })
	call("user, find the parent id", func(ctx context.Context) error { return drop(userRepo.FindWaliIDByUserID(ctx, parent)) })
	call("parent, find a parent", func(ctx context.Context) error { return drop(waliRepo.FindWaliByUserID(ctx, parent)) })
	call("parent, find a child", func(ctx context.Context) error { return drop(waliRepo.FindStudent(ctx, wali, &child)) })
	call("parent, find the first child", func(ctx context.Context) error { return drop(waliRepo.FindStudent(ctx, wali, nil)) })
	call("parent, latest mood", func(ctx context.Context) error { return drop(waliRepo.LatestMood(ctx, child, from, to)) })
	call("parent, count trash by type", func(ctx context.Context) error { return drop(waliRepo.CountTrashByType(ctx, child)) })
	call("parent, sum trash by type", func(ctx context.Context) error { return drop(waliRepo.SumTrashByType(ctx, child)) })
	call("parent, sum trash by type between", func(ctx context.Context) error {
		return drop(waliRepo.SumTrashByTypeBetween(ctx, child, from, to))
	})
	call("parent, count scans", func(ctx context.Context) error { return drop(waliRepo.CountScans(ctx, child, from, to)) })
	call("parent, find the guardian", func(ctx context.Context) error { return drop(waliRepo.FindGuardian(ctx, parent)) })
	call("parent, children", func(ctx context.Context) error { return drop(waliRepo.ListChildren(ctx, wali)) })
	call("parent, previous classes", func(ctx context.Context) error { return drop(waliRepo.PreviousClasses(ctx, wali, child)) })
	call("parent, moves", func(ctx context.Context) error { return drop(waliRepo.ListMoves(ctx, wali, child)) })

	// writing, where the statements around the write are the part that grows with the data
	call("auth, create a session", func(ctx context.Context) error {
		return auth.CreateRefreshToken(ctx, &entity.RefreshToken{
			ID: uuid.New(), UserID: parent, TokenHash: fmt.Sprintf("%x%x", md5.Sum([]byte("new")), md5.Sum([]byte("token"))),
			ExpiresAt: now.Add(7 * 24 * time.Hour), FamilyID: uuid.New(), SessionStartedAt: now,
		}, "x", 90*24*time.Hour)
	})
	call("auth, swap a refresh token", func(ctx context.Context) error {
		next := &entity.RefreshToken{
			ID: uuid.New(), UserID: parent, TokenHash: fmt.Sprintf("%x%x", md5.Sum([]byte("next")), md5.Sum([]byte("token"))),
			ExpiresAt: now.Add(7 * 24 * time.Hour), FamilyID: uuid.New(), SessionStartedAt: now,
		}

		return drop(auth.RotateRefreshToken(ctx, session, now, next, authrepository.RotationPolicy{
			SessionMax: 90 * 24 * time.Hour, ReuseLeeway: 10 * time.Second,
		}))
	})
	call("auth, swap a token that was used", func(ctx context.Context) error {
		next := &entity.RefreshToken{
			ID: uuid.New(), UserID: parent, TokenHash: fmt.Sprintf("%x%x", md5.Sum([]byte("again")), md5.Sum([]byte("token"))),
			ExpiresAt: now.Add(7 * 24 * time.Hour), FamilyID: uuid.New(), SessionStartedAt: now,
		}

		return drop(auth.RotateRefreshToken(ctx, session, now.Add(time.Minute), next, authrepository.RotationPolicy{
			SessionMax: 90 * 24 * time.Hour, ReuseLeeway: 10 * time.Second,
		}))
	})
	call("auth, delete a refresh token", func(ctx context.Context) error { return auth.DeleteRefreshToken(ctx, spare) })
	call("chat, get or create a conversation", func(ctx context.Context) error {
		low, high := teacher, parent
		if strings.Compare(low.String(), high.String()) > 0 {
			low, high = high, low
		}

		return drop2(chat.GetOrCreateConversation(ctx, low, high, now))
	})
	call("chat, get or create the room of a class", func(ctx context.Context) error {
		return drop2(chat.GetOrCreateClassRoom(ctx, class, now))
	})
	call("chat, send a message", func(ctx context.Context) error {
		return chat.CreateMessage(ctx, &entity.ChatMessage{ConversationID: chatID, SenderID: teacher, Body: "hello", CreatedAt: now})
	})
	call("chat, mark as read", func(ctx context.Context) error {
		return drop2(chat.MarkRead(ctx, chatID, parent, joined, messageID, now))
	})
	call("push, save a token", func(ctx context.Context) error {
		return pushRepo.SaveToken(ctx, parent, fmt.Sprintf("%x%x", md5.Sum([]byte("fresh")), md5.Sum([]byte("token"))), "android", now)
	})
	call("trash, claim a scan", func(ctx context.Context) error {
		return drop(trash.Claim(ctx, &entity.TrashScan{
			ID: uuid.New(), StudentID: child, TrashType: constants.TrashOrganik, ConfidenceScore: 0.9, PointsAwarded: 5, ScannedAt: now,
		}, class, 100, now.Add(-24*time.Hour), now.Add(24*time.Hour)))
	})
	call("guru, record the moods of a class", func(ctx context.Context) error {
		logs := []entity.MoodLog{
			{ID: uuid.New(), StudentID: child, RecordedByGuruID: guru, MoodType: constants.MoodSenang, ConfidenceScore: 1, Source: constants.SourceManualInput, RecordedAt: now},
			{ID: uuid.New(), StudentID: secondTwo, RecordedByGuruID: guru, MoodType: constants.MoodSedih, ConfidenceScore: 1, Source: constants.SourceManualInput, RecordedAt: now},
		}

		return drop(guruRepo.CreateMoodLogs(ctx, class, logs, gururepository.MoodDay{From: now.Add(-time.Hour), To: now.Add(time.Hour), Limit: 3}))
	})

	// the jobs that run on their own
	call("job, sweep the refresh tokens", func(ctx context.Context) error {
		return drop(sessions.NewSweeper(logged, 90*24*time.Hour).Sweep(ctx))
	})
	call("job, sweep the files", func(ctx context.Context) error {
		return drop(filecleanup.New(logged, s3.Disabled{}).Sweep(ctx))
	})
	call("job, sweep the idempotency keys", func(ctx context.Context) error {
		return drop(idempotency.New(logged, 24*time.Hour, 10*time.Second).Sweep(ctx))
	})
	call("job, claim an idempotency key", func(ctx context.Context) error {
		return drop2(idempotency.New(logged, 24*time.Hour, 10*time.Second).Claim(ctx, parent, "explain-key", []byte("fingerprint")))
	})
	call("ranking, recompute a class", func(ctx context.Context) error { return ranking.Recompute(logged.WithContext(ctx), class) })
	call("enrollment, requests of a class", func(ctx context.Context) error { return drop(enrollment.ListForClass(logged.WithContext(ctx), class)) })

	// the semester job. It is first asked on a minute when nothing has changed, then at the start of a semester, when it goes
	// through every class 50 at a time.
	job := season.New(logged, cacheStub{}, utc, []int{1, 7})
	call("season job, a minute in the middle of a semester", func(ctx context.Context) error {
		label := season.Label(now, utc, []int{1, 7})
		if err := logged.Session(&gorm.Session{Logger: gormlogger.Discard}).WithContext(ctx).
			Exec("UPDATE class_seasons SET season_label = ?", label).Error; err != nil {
			return err
		}

		return drop(job.WithClock(func() time.Time { return now }).Run(ctx))
	})
	call("season job, the start of a semester for every class", func(ctx context.Context) error {
		later := now.AddDate(0, 7, 0)
		job = job.WithClock(func() time.Time { return later })

		for {
			more, err := job.Run(ctx)
			if err != nil || !more {
				return err
			}
		}
	})

	// the deletes of an account run last, because they remove what the calls above read
	call("user, delete a parent account", func(ctx context.Context) error {
		return drop(userRepo.SoftDeleteAccount(ctx, id("wali5"), constants.RoleWali, "x", "", func(string) (string, bool) { return "", false }))
	})
	call("user, delete a teacher account", func(ctx context.Context) error {
		return drop(userRepo.SoftDeleteAccount(ctx, id("guru3"), constants.RoleGuru, "x", "guru4@example.test", func(string) (string, bool) { return "", false }))
	})
}

// cacheStub is the cache of the semester job, which only deletes a key, and a call to anything else panics on the nil interface
type cacheStub struct{ redis.CacheItf }

func (cacheStub) Del(context.Context, ...string) error { return nil }
