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

func NewWaliHandler(router fiber.Router, mw middleware.MiddlewareItf, classCodeLimiter fiber.Handler, useCase usecase.WaliUseCaseItf) {
	handler := WaliHandler{useCase: useCase}

	group := router.Group("/wali", mw.Authentication, mw.RequireRole(constants.RoleWali))
	idempotent := mw.Idempotent()

	group.Get("/dashboard", handler.Dashboard)
	group.Post("/guidance/apply", idempotent, handler.ApplyGuidance)
	group.Get("/child/:studentId", handler.Child)
	group.Get("/children", handler.Children)
	// the class code is the secret between a stranger and a class, so naming one is limited for the whole API
	group.Post("/children", idempotent, classCodeLimiter, handler.AddChild)

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

func (h *WaliHandler) Children(c fiber.Ctx) error {
	res, err := h.useCase.Children(c.Context(), middleware.UserIDFrom(c))
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *WaliHandler) AddChild(c fiber.Ctx) error {
	var req dto.AddChildRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.AddChild(c.Context(), middleware.UserIDFrom(c), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Child added", res)
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
