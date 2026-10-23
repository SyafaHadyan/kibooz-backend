package fiber

import (
	"sync"

	"github.com/gofiber/fiber/v3"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

// UploadGate lets only UPLOAD_CONCURRENCY photo uploads run at the same time in this copy of the API, and only
// UPLOAD_CONCURRENCY_PER_ACCOUNT of them for one account. It refuses the next one at once with a 429 that has the code of a rate
// limit and a Retry-After, so an app waits the way it does for any other limit.
// A photo is held in memory as the body, as the decoded text of it and as the cleaned copy, so a few uploads at the same time
// are what could use up the memory of a copy. Fiber has read the body of the request by the time this runs, so the gate bounds
// the work that follows and not the bodies that are waiting. It has to come after the authentication, so that only a signed in
// account can take a slot, and key names that account. A request with a body that is not larger than a JSON request may be holds
// no photo, such as a scan claim without one, so it does not take a slot and is never refused. The limit of the account is checked
// first, so a client that loops is refused by its own limit and does not take a slot of the copy that it cannot use. Call it once,
// because every call makes slots of its own.
func (f *Fiber) UploadGate(key func(fiber.Ctx) string) fiber.Handler {
	slots := make(chan struct{}, max(f.uploadSlots, 1))
	perAccount := max(f.uploadPerAccount, 1)

	var (
		mu   sync.Mutex
		held = map[string]int{}
	)

	// the entry of an account is removed with its last upload, so the map holds only the accounts that are uploading now
	take := func(account string) bool {
		mu.Lock()
		defer mu.Unlock()

		if held[account] >= perAccount {
			return false
		}

		held[account]++

		return true
	}

	give := func(account string) {
		mu.Lock()
		defer mu.Unlock()

		held[account]--
		if held[account] <= 0 {
			delete(held, account)
		}
	}

	return func(c fiber.Ctx) error {
		if len(c.Body()) <= constants.JSONBodyMaxBytes {
			return c.Next()
		}

		account := key(c)
		if !take(account) {
			return apperror.ErrUploadsAccountBusy
		}

		defer give(account)

		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()

			return c.Next()
		default:
			return apperror.ErrUploadsBusy
		}
	}
}
