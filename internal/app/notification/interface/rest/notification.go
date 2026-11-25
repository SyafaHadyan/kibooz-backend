// Package rest exposes in-app notifications over HTTP.
package rest

import (
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/notification/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type NotificationHandler struct {
	useCase usecase.NotificationUseCaseItf
}

func NewNotificationHandler(router fiber.Router, mw middleware.MiddlewareItf, useCase usecase.NotificationUseCaseItf) {
	handler := NotificationHandler{useCase: useCase}

	group := router.Group("/notifications", mw.Authentication, mw.RequireRole(constants.RoleGuru, constants.RoleWali))

	group.Get("", handler.List)
	group.Post("/read-all", handler.MarkAllRead)
	group.Post("/:notificationId/read", handler.MarkRead)
}

func (h *NotificationHandler) List(c fiber.Ctx) error {
	unreadOnly := false
	if raw := c.Query("unreadOnly"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return apperror.Validation(map[string]string{"unreadOnly": "must be true or false"})
		}

		unreadOnly = parsed
	}

	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.List(c.Context(), middleware.UserIDFrom(c), unreadOnly, page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *NotificationHandler) MarkRead(c fiber.Ctx) error {
	notificationID, err := uuid.Parse(c.Params("notificationId"))
	if err != nil {
		return apperror.Validation(map[string]string{"notificationId": "invalid UUID format"})
	}

	err = h.useCase.MarkRead(c.Context(), middleware.UserIDFrom(c), notificationID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Notification marked as read", nil)
}

func (h *NotificationHandler) MarkAllRead(c fiber.Ctx) error {
	err := h.useCase.MarkAllRead(c.Context(), middleware.UserIDFrom(c))
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "All notifications marked as read", nil)
}
