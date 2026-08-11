package bootstrap

import (
	"context"
	"legacy-messenger-control-plane/configs"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

type App struct {
	Config   *configs.Config
	Clients  *Clients
	UseCases *UseCases
	Handlers *Handlers
	Router   *gin.Engine
}

func NewApp(ctx context.Context) (*App, error) {

	// 로컬, Definition의 환경변수를 사용
	if err := godotenv.Load(".env"); err != nil {
		log.Println(".env not found, using system environment variables")
	}

	cfg, err := configs.Load()
	if err != nil {
		return nil, err
	}

	serviceRegistry, err := configs.NewServiceRegistry(cfg)
	if err != nil {
		return nil, err
	}

	clients, err := NewClients(ctx, cfg)
	if err != nil {
		return nil, err
	}

	useCases := NewUseCases(clients, cfg, serviceRegistry)
	handlers := NewHandlers(useCases)
	router := NewRouter(handlers)

	err = NewScheduler(ctx, useCases, cfg)
	if err != nil {
		return nil, err
	}

	return &App{
		Config:   cfg,
		Clients:  clients,
		UseCases: useCases,
		Handlers: handlers,
		Router:   router,
	}, nil
}
