package redis_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
)

// server speaks just enough of the Redis protocol for go-redis to talk to it, and answers each command the way reply says.
// An empty answer leaves the call hanging, which is how a Redis that stopped responding looks.
type server struct {
	listener net.Listener
	reply    atomic.Value
	commands atomic.Int64
}

func startServer(t *testing.T, reply func(args []string) string) *server {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s := &server{listener: listener}
	s.reply.Store(reply)

	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go s.serve(conn)
		}
	}()

	return s
}

func (s *server) answer(reply func(args []string) string) { s.reply.Store(reply) }

func (s *server) serve(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	for {
		args, err := readCommand(reader)
		if err != nil {
			return
		}

		var answer string

		switch strings.ToLower(args[0]) {
		case "hello":
			// go-redis falls back to the older protocol when this is refused
			answer = "-ERR unknown command 'hello'\r\n"
		case "client", "select", "auth":
			answer = "+OK\r\n"
		default:
			s.commands.Add(1)
			answer = s.reply.Load().(func([]string) string)(args)
		}

		if answer == "" {
			time.Sleep(10 * time.Second)

			return
		}

		_, _ = conn.Write([]byte(answer))
	}
}

func readCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	count, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "*")))
	if err != nil {
		return nil, err
	}

	args := make([]string, 0, count)

	for range count {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}

		size, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(header, "$")))
		if err != nil {
			return nil, err
		}

		buffer := make([]byte, size+2)
		if _, err := io.ReadFull(reader, buffer); err != nil {
			return nil, err
		}

		args = append(args, string(buffer[:size]))
	}

	return args, nil
}

func connect(t *testing.T, s *server) *redis.Redis {
	t.Helper()

	address := s.listener.Addr().(*net.TCPAddr)
	cache := redis.New(&env.Env{RedisAddress: address.IP.String(), RedisPort: uint(address.Port)})

	t.Cleanup(func() { _ = cache.Close() })

	return cache
}

func healthy(args []string) string {
	switch strings.ToLower(args[0]) {
	case "ping":
		return "+PONG\r\n"
	case "get":
		return "$5\r\nvalue\r\n"
	default:
		return "+OK\r\n"
	}
}

func TestOnlyOneCallTestsARedisThatWasDown(t *testing.T) {
	// a Redis that accepts connections and never answers, so the call that tests it is slow
	silent := startServer(t, func([]string) string { return "" })
	cache := connect(t, silent)
	require.False(t, cache.Available(), "the first call timed out")

	cache.EndPause()

	var (
		group       sync.WaitGroup
		refused     atomic.Int64
		longest     atomic.Int64
		started     = time.Now()
		callers     = 20
		testedAfter atomic.Int64
	)

	for range callers {
		group.Add(1)

		go func() {
			defer group.Done()

			_, _, err := cache.Get(context.Background(), "key")
			if errors.Is(err, redis.ErrUnavailable) {
				refused.Add(1)
				longest.Store(max(longest.Load(), int64(time.Since(started))))

				return
			}

			testedAfter.Store(int64(time.Since(started)))
		}()
	}

	group.Wait()

	require.EqualValues(t, callers-1, refused.Load(), "all but the call that tests Redis are refused at once")
	require.Less(t, time.Duration(longest.Load()), 200*time.Millisecond, "the refused calls did not wait for the slow one")
	require.Greater(t, time.Duration(testedAfter.Load()), 200*time.Millisecond, "the call that tested Redis waited for it")
	require.False(t, cache.Available(), "Redis did not answer, so the pause starts again")
}

func TestARedisThatAnswersAgainIsUsedByEveryCall(t *testing.T) {
	flaky := startServer(t, func([]string) string { return "" })
	cache := connect(t, flaky)
	require.False(t, cache.Available())

	flaky.answer(healthy)
	cache.EndPause()

	value, found, err := cache.Get(context.Background(), "key")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "value", value)

	require.True(t, cache.Available())

	for range 5 {
		_, _, err = cache.Get(context.Background(), "key")
		require.NoError(t, err, "the breaker is closed, so no call has to wait for a test")
	}
}

func TestAnAnswerThatIsAnErrorAboutTheCommandDoesNotOpenTheBreaker(t *testing.T) {
	for name, answer := range map[string]string{
		"a key of the wrong type":     "-WRONGTYPE Operation against a key holding the wrong kind of value\r\n",
		"a user without permission":   "-NOPERM this user has no permissions to run the 'get' command\r\n",
		"a command the version lacks": "-ERR syntax error\r\n",
	} {
		s := startServer(t, healthy)
		cache := connect(t, s)

		s.answer(func(args []string) string {
			if strings.ToLower(args[0]) == "get" {
				return answer
			}

			return healthy(args)
		})

		for range 3 {
			_, _, err := cache.Get(context.Background(), "key")
			require.Error(t, err, name)
			require.NotErrorIs(t, err, redis.ErrUnavailable, name)
		}

		require.True(t, cache.Available(), name)
		require.EqualValues(t, 3, s.commands.Load()-1, "every call still reached Redis, %s", name)
	}
}

func TestAnAnswerThatSaysRedisCannotServeOpensTheBreaker(t *testing.T) {
	for name, answer := range map[string]string{
		"a replica":             "-READONLY You can't write against a read only replica.\r\n",
		"a dataset that loads":  "-LOADING Redis is loading the dataset in memory\r\n",
		"memory that is full":   "-OOM command not allowed when used memory > 'maxmemory'.\r\n",
		"a script that runs on": "-BUSY Redis is busy running a script.\r\n",
	} {
		s := startServer(t, healthy)
		cache := connect(t, s)

		s.answer(func(args []string) string {
			if strings.ToLower(args[0]) == "set" {
				return answer
			}

			return healthy(args)
		})

		require.Error(t, cache.Set(context.Background(), "key", "value", time.Minute), name)
		require.False(t, cache.Available(), name)
		require.ErrorIs(t, cache.Set(context.Background(), "key", "value", time.Minute), redis.ErrUnavailable, name)
	}
}

func TestACallThatTheCallerGaveUpOnSaysNothingAboutRedis(t *testing.T) {
	s := startServer(t, healthy)
	cache := connect(t, s)

	t.Run("a deadline that was over", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()

		time.Sleep(time.Millisecond)

		_, _, err := cache.Get(ctx, "key")
		require.Error(t, err)
		require.True(t, cache.Available())
	})

	t.Run("a call that was cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, _, err := cache.Get(ctx, "key")
		require.Error(t, err)
		require.True(t, cache.Available())
	})

	t.Run("the next call goes through", func(t *testing.T) {
		_, _, err := cache.Get(context.Background(), "key")
		require.NoError(t, err)
	})
}

func TestATimeoutOfTheClientOpensTheBreakerWhileTheCallerStillWaits(t *testing.T) {
	s := startServer(t, func([]string) string { return "" })
	cache := connect(t, s)

	// the context of the caller is alive for a long time, so the timeout that ends the call is the one of the client
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	_, _, err := cache.Get(ctx, "key")
	require.Error(t, err)
	require.NoError(t, ctx.Err())
	require.False(t, cache.Available())
}

func TestAvailableDoesNotTakeTheCallThatTestsRedis(t *testing.T) {
	silent := startServer(t, func([]string) string { return "" })
	cache := connect(t, silent)

	cache.EndPause()

	for range 5 {
		require.True(t, cache.Available(), "looking is not testing")
	}

	_, _, err := cache.Get(context.Background(), "key")
	require.NotErrorIs(t, err, redis.ErrUnavailable, "the first real call is still the one that tests Redis")
}
