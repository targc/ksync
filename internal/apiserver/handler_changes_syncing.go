package apiserver

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	ksync "github.com/targc/ksync/pkg"
)

func (s *Server) setSyncing(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: "invalid id"})
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
		Update("syncing_change_custom_resource_id", id).
		Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(errorResponse{Error: "failed to set syncing"})
	}

	return c.SendStatus(fiber.StatusOK)
}
