// Package pagination reads the page and limit query parameters shared by the list endpoints
package pagination

import (
	"strconv"

	"github.com/gofiber/fiber/v3"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

const (
	DefaultLimit = 20
	MaxLimit     = 50
	MaxPage      = 10000
)

type Params struct {
	Page  int
	Limit int
}

// Offset is the number of rows before the requested page
func (p Params) Offset() int {
	return (p.Page - 1) * p.Limit
}

// Parse reads page and limit from the query string, a missing value falls back to the default
func Parse(c fiber.Ctx) (Params, error) {
	return parse(c.Query("page"), c.Query("limit"))
}

func parse(page string, limit string) (Params, error) {
	params := Params{Page: 1, Limit: DefaultLimit}
	details := map[string]string{}

	if page != "" {
		value, err := strconv.Atoi(page)
		if err != nil || value < 1 || value > MaxPage {
			details["page"] = "must be a whole number from 1 to " + strconv.Itoa(MaxPage)
		} else {
			params.Page = value
		}
	}

	if limit != "" {
		value, err := strconv.Atoi(limit)
		if err != nil || value < 1 || value > MaxLimit {
			details["limit"] = "must be a whole number from 1 to " + strconv.Itoa(MaxLimit)
		} else {
			params.Limit = value
		}
	}

	if len(details) > 0 {
		return Params{}, apperror.Validation(details)
	}

	return params, nil
}
