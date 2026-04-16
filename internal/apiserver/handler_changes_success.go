package apiserver

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	ksync "github.com/targc/ksync/pkg"
)

func (s *Server) setSuccess(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: "invalid id"})
	}

	cluster := c.Locals("cluster").(string)

	var change ksync.ChangeCustomResource
	if err := s.db.WithContext(c.Context()).First(&change, "id = ?", id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{Error: "change not found"})
	}

	if err := s.db.WithContext(c.Context()).Delete(&ksync.ChangeCustomResource{}, "id = ?", id).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(errorResponse{Error: "failed to delete change"})
	}

	updates := map[string]interface{}{
		"syncing_change_custom_resource_id": nil,
		"last_change_custom_resource_id":    change.ID,
		"last_sync_error":                   nil,
	}
	if change.Action == ksync.ChangeCustomResourceActionApply {
		updates["json"] = change.JSON
	}
	if change.Action == ksync.ChangeCustomResourceActionDelete {
		updates["deleted_at"] = time.Now()
	}

	err = s.db.
		WithContext(c.Context()).
		Model(&ksync.CustomResource{}).
		Where("id = ? AND cluster = ?", change.CustomResourceID, cluster).
		Updates(updates).
		Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(errorResponse{Error: "failed to update resource"})
	}

	return c.SendStatus(fiber.StatusOK)
}
