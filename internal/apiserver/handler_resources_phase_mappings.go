package apiserver

import (
	"github.com/gofiber/fiber/v3"
	ksync "github.com/targc/ksync/pkg"
)

type phaseMappingItem struct {
	Kind   string `json:"kind"`
	Phase  string `json:"phase"`
	Status string `json:"status"`
}

func (s *Server) listPhaseMappings(c fiber.Ctx) error {
	cluster := c.Locals("cluster").(string)

	var mappings []ksync.PhaseMapping
	err := s.db.
		WithContext(c.Context()).
		Where("cluster = ?", cluster).
		Find(&mappings).
		Error
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(errorResponse{Error: "failed to fetch phase mappings"})
	}

	result := make([]phaseMappingItem, len(mappings))
	for i, m := range mappings {
		result[i] = phaseMappingItem{Kind: m.Kind, Phase: m.Phase, Status: m.Status}
	}

	return c.JSON(result)
}
