package apiserver

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	ksync "github.com/targc/ksync/pkg"
)

type setErrorRequest struct {
	Error string `json:"error"`
}

func (s *Server) setError(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: "invalid id"})
	}

	var req setErrorRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: "invalid request"})
	}

	cluster := c.Locals("cluster").(string)

	var change ksync.ChangeCustomResource
	if err := s.db.WithContext(c.Context()).First(&change, "id = ?", id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{Error: "change not found"})
	}

	err = s.db.
		WithContext(c.Context()).
		Model(&ksync.CustomResource{}).
		Where("id = ? AND cluster = ?", change.CustomResourceID, cluster).
		Updates(map[string]interface{}{
			"syncing_change_custom_resource_id": nil,
			"last_sync_error":                   req.Error,
		}).
		Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(errorResponse{Error: "failed to update error"})
	}

	return c.SendStatus(fiber.StatusOK)
}
