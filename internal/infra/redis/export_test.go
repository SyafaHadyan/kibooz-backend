package redis

import "time"

// EndPause makes the pause of an open breaker over, so the next call is the one that tests Redis
func (r *Redis) EndPause() {
	r.downUntil.Store(time.Now().Add(-time.Second).UnixNano())
}
