package apiserver

import (
	"github.com/gofiber/fiber/v3"
	ksync "github.com/targc/ksync/pkg"
	"gorm.io/gorm"
)

type errorResponse struct {
	Error string `json:"error"`
}

type Server struct {
	db *gorm.DB
}

func NewServer(db *gorm.DB) *Server {
	return &Server{db: db}
}

func (s *Server) SetupRoutes(app *fiber.App) {
	api := app.Group("/api/v1")
	api.Use(s.authMiddleware())
	api.Get("/changes", s.listChanges)
	api.Post("/changes/:id/syncing", s.setSyncing)
	api.Post("/changes/:id/success", s.setSuccess)
	api.Post("/changes/:id/error", s.setError)
}

func (s *Server) authMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		auth := c.Get("Authorization")
		if len(auth) < 8 || auth[:7] != "Bearer " {
			return c.Status(fiber.StatusUnauthorized).JSON(errorResponse{Error: "missing token"})
		}
		token := auth[7:]

		var apiToken ksync.ApiToken
		err := s.db.
			WithContext(c.Context()).
			Where("token = ?", token).
			First(&apiToken).
			Error
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(errorResponse{Error: "invalid token"})
		}

		c.Locals("cluster", apiToken.Cluster)
		return c.Next()
	}
}
