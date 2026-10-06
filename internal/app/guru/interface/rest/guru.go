// Package rest exposes the teacher portal over HTTP
package rest

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type GuruHandler struct {
	useCase usecase.GuruUseCaseItf
}

func NewGuruHandler(router fiber.Router, mw middleware.MiddlewareItf, useCase usecase.GuruUseCaseItf) {
	handler := GuruHandler{useCase: useCase}

	group := router.Group("/guru", mw.Authentication, mw.RequireRole(constants.RoleGuru))

	group.Get("/dashboard", handler.Dashboard)
	group.Post("/mood/log", handler.LogMood)
	group.Get("/mood/analytics", handler.MoodAnalytics)
}

func (h *GuruHandler) Dashboard(c fiber.Ctx) error {
	classID, err := optionalUUID(c, "classId")
	if err != nil {
		return err
	}

	res, err := h.useCase.Dashboard(c.Context(), middleware.UserIDFrom(c), classID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *GuruHandler) LogMood(c fiber.Ctx) error {
	var req dto.LogMoodRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.LogMood(c.Context(), middleware.UserIDFrom(c), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Student mood log saved", res)
}

func (h *GuruHandler) MoodAnalytics(c fiber.Ctx) error {
	classID, err := optionalUUID(c, "classId")
	if err != nil {
		return err
	}

	res, err := h.useCase.MoodAnalytics(c.Context(), middleware.UserIDFrom(c), classID, c.Query("range"))
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func optionalUUID(c fiber.Ctx, name string) (*uuid.UUID, error) {
	raw := c.Query(name)
	if raw == "" {
		return nil, nil
	}

	parsed, err := uuid.Parse(raw)
	if err != nil {
		return nil, apperror.Validation(map[string]string{name: "invalid UUID format"})
	}

	return &parsed, nil
}
