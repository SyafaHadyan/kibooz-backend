package redis

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

func TestOptionsUsePlainTCPByDefault(t *testing.T) {
	opts := newOptions(&env.Env{RedisAddress: "cache.internal", RedisPort: 6379})

	require.Equal(t, "cache.internal:6379", opts.Addr)
	require.Nil(t, opts.TLSConfig)
}

func TestOptionsEnableVerifiedTLSWhenRequested(t *testing.T) {
	opts := newOptions(&env.Env{RedisAddress: "cache.example.com", RedisPort: 6380, RedisTLS: true})

	require.NotNil(t, opts.TLSConfig)
	require.Equal(t, "cache.example.com", opts.TLSConfig.ServerName)
	require.Equal(t, uint16(tls.VersionTLS12), opts.TLSConfig.MinVersion)
	require.False(t, opts.TLSConfig.InsecureSkipVerify)
}
