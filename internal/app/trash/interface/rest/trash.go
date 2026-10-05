// Package rest exposes the gamification endpoints over HTTP
package rest

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/trash/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type TrashHandler struct {
	useCase usecase.TrashUseCaseItf
}

func NewTrashHandler(router fiber.Router, mw middleware.MiddlewareItf, useCase usecase.TrashUseCaseItf) {
	handler := TrashHandler{useCase: useCase}

	router.Post("/trash/scan-claim", mw.Authentication, mw.RequireRole(constants.RoleWali), handler.ScanClaim)
	router.Get("/leaderboard", mw.Authentication, mw.RequireRole(constants.RoleWali, constants.RoleGuru), handler.Leaderboard)
}

func (h *TrashHandler) ScanClaim(c fiber.Ctx) error {
	var req dto.ScanClaimRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.ScanClaim(c.Context(), middleware.UserIDFrom(c), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Poin pilah sampah berhasil diklaim!", res)
}

func (h *TrashHandler) Leaderboard(c fiber.Ctx) error {
	var classID *uuid.UUID

	if raw := c.Query("classId"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return apperror.Validation(map[string]string{"classId": "format UUID tidak valid"})
		}

		classID = &parsed
	}

	res, err := h.useCase.Leaderboard(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}
