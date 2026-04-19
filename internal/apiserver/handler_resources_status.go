package apiserver

import (
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type statusUpdateItem struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func (s *Server) bulkUpdateStatus(c fiber.Ctx) error {
	cluster := c.Locals("cluster").(string)

	var items []statusUpdateItem
	if err := c.Bind().JSON(&items); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: "invalid request body"})
	}
	if len(items) > 100 {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: "too many items"})
	}

	ctx := c.Context()
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			continue
		}

		err = s.db.
			WithContext(ctx).
			Exec(`
				INSERT INTO ksync_custom_resource_statuses (custom_resource_id, status, updated_at)
				SELECT id, ?, NOW() FROM ksync_custom_resources
				WHERE id = ? AND cluster = ? AND deleted_at IS NULL
				ON CONFLICT (custom_resource_id) DO UPDATE SET status = EXCLUDED.status, updated_at = NOW()
			`, item.Status, id, cluster).
			Error
		if err != nil {
			slog.Error("failed to upsert status", "id", id, "error", err)
		}
	}

	return c.SendStatus(fiber.StatusNoContent)
}
