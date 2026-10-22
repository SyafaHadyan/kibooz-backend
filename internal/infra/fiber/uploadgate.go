package fiber

import (
	"github.com/gofiber/fiber/v3"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

// UploadGate lets only UPLOAD_CONCURRENCY photo uploads run at the same time in this copy of the API, and refuses the next one
// at once with a 429 that has the code of a rate limit and a Retry-After, so an app waits the way it does for any other limit.
// A photo is held in memory as the body, as the decoded text of it and as the cleaned copy, so a few uploads at the same time
// are what could use up the memory of a copy. Fiber has read the body of the request by the time this runs, so the gate bounds
// the work that follows and not the bodies that are waiting. It has to come after the authentication, so that only a signed in
// account can take a slot. A request with a body that is not larger than a JSON request may be holds no photo, such as a scan
// claim without one, so it does not take a slot and is never refused. Call it once, because every call makes slots of its own.
func (f *Fiber) UploadGate() fiber.Handler {
	slots := make(chan struct{}, max(f.uploadSlots, 1))

	return func(c fiber.Ctx) error {
		if len(c.Body()) <= constants.JSONBodyMaxBytes {
			return c.Next()
		}

		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()

			return c.Next()
		default:
			return apperror.ErrUploadsBusy
		}
	}
}
