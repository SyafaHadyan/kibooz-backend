// Package rest exposes the chat between teachers and parents over HTTP
package rest

import (
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/chat/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type ChatHandler struct {
	useCase usecase.ChatUseCaseItf
}

func NewChatHandler(router fiber.Router, mw middleware.MiddlewareItf, useCase usecase.ChatUseCaseItf) {
	handler := ChatHandler{useCase: useCase}

	group := router.Group("/chat", mw.Authentication, mw.RequireRole(constants.RoleGuru, constants.RoleWali))

	// a repeat of a request that already wrote a message gets the first response again
	idempotent := mw.Idempotent()

	group.Get("/contacts", handler.ListContacts)
	group.Get("/conversations", handler.ListConversations)
	group.Post("/conversations", handler.StartConversation)
	group.Get("/conversations/:conversationId/messages", handler.ListMessages)
	group.Post("/conversations/:conversationId/messages", idempotent, handler.SendMessage)
	group.Delete("/conversations/:conversationId/messages/:messageId", handler.DeleteMessage)
	group.Put("/conversations/:conversationId/read", handler.MarkRead)
}

func (h *ChatHandler) ListContacts(c fiber.Ctx) error {
	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ListContacts(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *ChatHandler) ListConversations(c fiber.Ctx) error {
	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ListConversations(c.Context(), middleware.UserIDFrom(c), page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *ChatHandler) StartConversation(c fiber.Ctx) error {
	var req dto.StartChatRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, created, err := h.useCase.StartConversation(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), req)
	if err != nil {
		return err
	}

	// the first call makes the conversation and every later call for the same pair returns it
	if created {
		return response.JSON(c, http.StatusCreated, "Conversation started", res)
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *ChatHandler) ListMessages(c fiber.Ctx) error {
	conversationID, err := pathUUID(c, "conversationId")
	if err != nil {
		return err
	}

	after, before, limit, err := messageWindow(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ListMessages(c.Context(), middleware.UserIDFrom(c), conversationID, after, before, limit)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *ChatHandler) SendMessage(c fiber.Ctx) error {
	conversationID, err := pathUUID(c, "conversationId")
	if err != nil {
		return err
	}

	var req dto.SendChatMessageRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.SendMessage(c.Context(), middleware.UserIDFrom(c), conversationID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Message sent", res)
}

func (h *ChatHandler) DeleteMessage(c fiber.Ctx) error {
	conversationID, err := pathUUID(c, "conversationId")
	if err != nil {
		return err
	}

	messageID, err := strconv.ParseInt(c.Params("messageId"), 10, 64)
	if err != nil || messageID < 1 {
		return apperror.Validation(map[string]string{"messageId": "must be a message id"})
	}

	res, err := h.useCase.DeleteMessage(c.Context(), middleware.UserIDFrom(c), conversationID, messageID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Message deleted", res)
}

func (h *ChatHandler) MarkRead(c fiber.Ctx) error {
	conversationID, err := pathUUID(c, "conversationId")
	if err != nil {
		return err
	}

	var req dto.MarkChatReadRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.MarkRead(c.Context(), middleware.UserIDFrom(c), conversationID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

// messageWindow reads after, before and limit from the query string. A missing cursor stays nil and a missing limit falls back
// to the default, and the use case checks the ranges.
func messageWindow(c fiber.Ctx) (*int64, *int64, int, error) {
	details := map[string]string{}

	read := func(name string) *int64 {
		raw := c.Query(name)
		if raw == "" {
			return nil
		}

		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			details[name] = "must be a whole number"

			return nil
		}

		return &value
	}

	after, before := read("after"), read("before")
	limit := usecase.DefaultMessageLimit

	if raw := c.Query("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			details["limit"] = "must be a whole number from 1 to " + strconv.Itoa(usecase.MaxMessageLimit)
		} else {
			limit = value
		}
	}

	if len(details) > 0 {
		return nil, nil, 0, apperror.Validation(details)
	}

	return after, before, limit, nil
}

func pathUUID(c fiber.Ctx, name string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.Nil, apperror.Validation(map[string]string{name: "invalid UUID format"})
	}

	return parsed, nil
}
