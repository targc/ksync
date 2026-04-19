package apiserver

import (
	"github.com/gofiber/fiber/v3"
	ksync "github.com/targc/ksync/pkg"
)

type gvrItem struct {
	APIVersion string `json:"api_version" gorm:"column:api_version"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
}

func (s *Server) listGVRs(c fiber.Ctx) error {
	cluster := c.Locals("cluster").(string)

	var items []gvrItem
	err := s.db.
		WithContext(c.Context()).
		Model(&ksync.CustomResource{}).
		Select("DISTINCT api_version, kind, namespace").
		Where("cluster = ? AND deleted_at IS NULL", cluster).
		Scan(&items).
		Error
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(errorResponse{Error: "failed to fetch GVRs"})
	}

	return c.JSON(items)
}
