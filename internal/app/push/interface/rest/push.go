// Package rest exposes the push tokens over HTTP
package rest

import (
	"net/http"

	"github.com/gofiber/fiber/v3"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/push/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type PushHandler struct {
	useCase usecase.PushUseCaseItf
}

func NewPushHandler(router fiber.Router, mw middleware.MiddlewareItf, useCase usecase.PushUseCaseItf) {
	handler := PushHandler{useCase: useCase}

	group := router.Group("/push", mw.Authentication, mw.RequireRole(constants.RoleGuru, constants.RoleWali))

	group.Post("/tokens", handler.Register)
	group.Delete("/tokens", handler.Remove)
}

func (h *PushHandler) Register(c fiber.Ctx) error {
	var req dto.PushTokenRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	err = h.useCase.Register(c.Context(), middleware.UserIDFrom(c), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Push token saved", nil)
}

func (h *PushHandler) Remove(c fiber.Ctx) error {
	var req dto.RemovePushTokenRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	err = h.useCase.Remove(c.Context(), middleware.UserIDFrom(c), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Push token removed", nil)
}
