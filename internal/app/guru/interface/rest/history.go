package rest

import (
	"net/http"

	"github.com/gofiber/fiber/v3"

	"github.com/kiboozapp/kibooz-backend/internal/middleware"
	"github.com/kiboozapp/kibooz-backend/internal/pagination"
	"github.com/kiboozapp/kibooz-backend/internal/response"
)

func (h *GuruHandler) ClassHistory(c fiber.Ctx) error {
	classID, err := optionalUUID(c, "classId")
	if err != nil {
		return err
	}

	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ClassHistory(c.Context(), middleware.UserIDFrom(c), classID, c.Query("type"), page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}
