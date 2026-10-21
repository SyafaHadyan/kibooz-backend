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
	group.Patch("/profile", handler.UpdateProfile)
	group.Get("/child/:studentId", handler.Child)
	group.Get("/children", handler.Children)
	group.Patch("/children/:studentId", handler.UpdateChild)
	group.Delete("/children/:studentId", handler.DeleteChild)
	// the class code is the secret between a stranger and a class, so naming one is limited for every caller of this route
	group.Post("/children", idempotent, classCodeLimiter, handler.AddChild)
	// a move names a class code as well, so it has a limit of its own, counted per route like the one above
	group.Post("/children/:studentId/move-requests", idempotent, classCodeLimiter, handler.RequestMove)
	group.Get("/children/:studentId/move-requests", handler.MoveRequests)
	group.Get("/children/:studentId/previous-classes", handler.PreviousClasses)
	group.Delete("/children/:studentId/move-requests/:requestId", handler.CancelMove)

	router.Get("/trash/stats", mw.Authentication, mw.RequireRole(constants.RoleWali), handler.TrashStats)
}

func (h *WaliHandler) UpdateProfile(c fiber.Ctx) error {
	var req dto.UpdateWaliProfileRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.UpdateProfile(c.Context(), middleware.UserIDFrom(c), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Profile updated", res)
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

func (h *WaliHandler) UpdateChild(c fiber.Ctx) error {
	studentID, err := uuid.Parse(c.Params("studentId"))
	if err != nil {
		return apperror.Validation(map[string]string{"studentId": "invalid UUID format"})
	}

	var req dto.UpdateChildRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.UpdateChild(c.Context(), middleware.UserIDFrom(c), studentID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Child updated", res)
}

func (h *WaliHandler) DeleteChild(c fiber.Ctx) error {
	studentID, err := uuid.Parse(c.Params("studentId"))
	if err != nil {
		return apperror.Validation(map[string]string{"studentId": "invalid UUID format"})
	}

	err = h.useCase.DeleteChild(c.Context(), middleware.UserIDFrom(c), studentID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Child deleted", nil)
}

func (h *WaliHandler) TrashStats(c fiber.Ctx) error {
	studentID, err := optionalStudentID(c)
	if err != nil {
		return err
	}

	period, ok := constants.ParsePeriod(c.Query("period"))
	if !ok {
		return apperror.Validation(map[string]string{"period": "must be today, week or month"})
	}

	res, err := h.useCase.TrashStats(c.Context(), middleware.UserIDFrom(c), studentID, period)
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

func (h *WaliHandler) RequestMove(c fiber.Ctx) error {
	studentID, err := uuid.Parse(c.Params("studentId"))
	if err != nil {
		return apperror.Validation(map[string]string{"studentId": "invalid UUID format"})
	}

	var req dto.MoveRequestInput

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.RequestMove(c.Context(), middleware.UserIDFrom(c), studentID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Move requested", res)
}

func (h *WaliHandler) MoveRequests(c fiber.Ctx) error {
	studentID, err := uuid.Parse(c.Params("studentId"))
	if err != nil {
		return apperror.Validation(map[string]string{"studentId": "invalid UUID format"})
	}

	res, err := h.useCase.MoveRequests(c.Context(), middleware.UserIDFrom(c), studentID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *WaliHandler) CancelMove(c fiber.Ctx) error {
	studentID, err := uuid.Parse(c.Params("studentId"))
	if err != nil {
		return apperror.Validation(map[string]string{"studentId": "invalid UUID format"})
	}

	requestID, err := uuid.Parse(c.Params("requestId"))
	if err != nil {
		return apperror.Validation(map[string]string{"requestId": "invalid UUID format"})
	}

	err = h.useCase.CancelMove(c.Context(), middleware.UserIDFrom(c), studentID, requestID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Move request cancelled", nil)
}

func (h *WaliHandler) PreviousClasses(c fiber.Ctx) error {
	studentID, err := uuid.Parse(c.Params("studentId"))
	if err != nil {
		return apperror.Validation(map[string]string{"studentId": "invalid UUID format"})
	}

	res, err := h.useCase.PreviousClasses(c.Context(), middleware.UserIDFrom(c), studentID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}
