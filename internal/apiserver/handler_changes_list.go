package apiserver

import (
	"github.com/gofiber/fiber/v3"
	ksync "github.com/targc/ksync/pkg"
)

func (s *Server) listChanges(c fiber.Ctx) error {
	cluster := c.Locals("cluster").(string)

	var changes []ksync.SyncChange
	err := s.db.
		WithContext(c.Context()).
		Raw(`
			SELECT DISTINCT ON (c.custom_resource_id)
				c.*,
				r.api_version AS cr_api_version,
				r.kind AS cr_kind,
				r.namespace AS cr_namespace,
				r.name AS cr_name
			FROM ksync_change_custom_resources c
			JOIN ksync_custom_resources r ON r.id = c.custom_resource_id
			WHERE r.cluster = ?
			ORDER BY c.custom_resource_id, c.id
			LIMIT 100
		`, cluster).
		Scan(&changes).
		Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(errorResponse{Error: "failed to list changes"})
	}

	return c.JSON(changes)
}
