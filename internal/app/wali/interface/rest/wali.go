// Package rest exposes the parent portal over HTTP
package rest

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/wali/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type WaliHandler struct {
	useCase usecase.WaliUseCaseItf
}

func NewWaliHandler(router fiber.Router, mw middleware.MiddlewareItf, useCase usecase.WaliUseCaseItf) {
	handler := WaliHandler{useCase: useCase}

	group := router.Group("/wali", mw.Authentication, mw.RequireRole(constants.RoleWali))

	group.Get("/dashboard", handler.Dashboard)
	group.Post("/guidance/apply", handler.ApplyGuidance)
	group.Get("/child/:studentId", handler.Child)

	router.Get("/trash/stats", mw.Authentication, mw.RequireRole(constants.RoleWali), handler.TrashStats)
}

func (h *WaliHandler) Dashboard(c fiber.Ctx) error {
	studentID, err := optionalStudentID(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.Dashboard(c.Context(), middleware.UserIDFrom(c), studentID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *WaliHandler) ApplyGuidance(c fiber.Ctx) error {
	var req dto.ApplyGuidanceRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	err = h.useCase.ApplyGuidance(c.Context(), middleware.UserIDFrom(c), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Handling status recorded", nil)
}

func (h *WaliHandler) Child(c fiber.Ctx) error {
	studentID, err := uuid.Parse(c.Params("studentId"))
	if err != nil {
		return apperror.Validation(map[string]string{"studentId": "invalid UUID format"})
	}

	res, err := h.useCase.Child(c.Context(), middleware.UserIDFrom(c), studentID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *WaliHandler) TrashStats(c fiber.Ctx) error {
	studentID, err := optionalStudentID(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.TrashStats(c.Context(), middleware.UserIDFrom(c), studentID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func optionalStudentID(c fiber.Ctx) (*uuid.UUID, error) {
	raw := c.Query("studentId")
	if raw == "" {
		return nil, nil
	}

	parsed, err := uuid.Parse(raw)
	if err != nil {
		return nil, apperror.Validation(map[string]string{"studentId": "invalid UUID format"})
	}

	return &parsed, nil
}
