package rest

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/kiboozapp/kibooz-backend/internal/apperror"
	"github.com/kiboozapp/kibooz-backend/internal/middleware"
	"github.com/kiboozapp/kibooz-backend/internal/pagination"
	"github.com/kiboozapp/kibooz-backend/internal/response"
)

func (h *WaliHandler) ChildHistory(c fiber.Ctx) error {
	studentID, err := uuid.Parse(c.Params("studentId"))
	if err != nil {
		return apperror.Validation(map[string]string{"studentId": "invalid UUID format"})
	}

	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ChildHistory(c.Context(), middleware.UserIDFrom(c), studentID, c.Query("type"), page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}
