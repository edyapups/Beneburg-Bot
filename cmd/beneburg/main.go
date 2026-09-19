package main

import (
	"beneburg/pkg/database"
	"beneburg/pkg/middleware"
	"beneburg/pkg/telegram"
	"beneburg/pkg/views"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	logger, _ := zap.NewDevelopment()
	defer func(logger *zap.Logger) {
		_ = logger.Sync()
	}(logger)

	err := run(logger)
	if err != nil {
		logger.Fatal(err.Error())
	}
}

func run(logger *zap.Logger) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config, err := loadConfig()
	if err != nil {
		return err
	}

	// Creating database connection
	db, err := database.NewDatabase(config.Database.DataSourceName, logger.Named("database"))
	if err != nil {
		return err
	}
	defer db.Close()
	// Making migrations
	err = db.Migrate(ctx)
	if err != nil {
		return err
	}

	// Stop if only migrations are needed
	if config.Database.OnlyMakeMigrations {
		logger.Info("Migrations were made, exiting...")
		return nil
	}

	// Configuring bot
	var SendFunc telegram.TelegramBotSendFunc
	if token := config.Telegram.Token; token != "" {
		botAPI, err := tgbotapi.NewBotAPI(token)
		if err != nil {
			return err
		}
		bot := telegram.NewBot(ctx, botAPI, db, config.Telegram.AdminID, config.Telegram.GroupID, config.Telegram.InviteLink, config.domain)
		SendFunc = bot.GetSendFunc()
		logger = logger.WithOptions(zap.Hooks(func(entry zapcore.Entry) error {
			if entry.Level < zapcore.WarnLevel {
				return nil
			}
			msg := tgbotapi.NewMessage(config.Telegram.AdminID, fmt.Sprintf("%s: %s (%s)", entry.Level, entry.Message, entry.Caller))
			SendFunc(msg)
			return nil
		}))

		bot.SetLogger(logger.Named("telegram"))
		bot.Start()
	}

	// Log panic
	defer func() {
		if r := recover(); r != nil {
			logger.Panic("panic", zap.Any("panic", r))
		}
	}()

	// Configuring gin
	router := gin.Default()
	if config.trustedProxy != "" {
		err := router.SetTrustedProxies(strings.Split(config.trustedProxy, ","))
		if err != nil {
			return err
		}
	}
	router.TrustedPlatform = "X-Real-IP"
	router.Static("/assets", "./assets")
	router.LoadHTMLGlob("templates/*")
	router.Use(cors.Default())
	router.GET("/healthz", func(request *gin.Context) {
		pingContext, cancelPing := context.WithTimeout(request.Request.Context(), 2*time.Second)
		defer cancelPing()
		if err := db.Ping(pingContext); err != nil {
			request.Status(http.StatusServiceUnavailable)
			return
		}
		request.Status(http.StatusNoContent)
	})

	// Configuring API
	var tokenAuthMiddleware middleware.TokenAuth
	if config.noAuth {
		tokenAuthMiddleware = middleware.NewDevTokenAuth()
	} else {
		tokenAuthMiddleware = middleware.NewTokenAuth(db, logger.Named("TokenAuthMiddleware"))
	}

	// Configuring groups
	loginGroup := router.Group("/login")
	profileGroup := router.Group("/profile")
	mainGroup := router.Group("/")

	// TokenAuthMiddleware
	mainGroup.Use(tokenAuthMiddleware.Auth)
	profileGroup.Use(tokenAuthMiddleware.Auth)

	// ProfileRedirectMiddleware
	mainGroup.Use(middleware.ProfileRedirectMiddleware())

	// Views
	viewsModule := views.NewViews(db, logger.Named("views"), SendFunc, config.Telegram.AdminID, config.Telegram.GroupID, config.domain)
	viewsModule.RegisterRoutes(mainGroup)
	viewsModule.RegisterLogin(loginGroup)
	viewsModule.RegisterProfile(profileGroup)

	// Starting server
	httpServer := &http.Server{Addr: ":8080", Handler: router}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpServer.ListenAndServe()
	}()

	logger.Info("Server started")
	logger.Info("All ready")

	// Waiting for signal
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-serverErrors:
		return fmt.Errorf("HTTP server stopped: %w", err)
	case sig := <-sigs:
		logger.Info("Received signal", zap.String("signal", sig.String()))
		cancel()
		shutdownContext, stop := context.WithTimeout(context.Background(), 4*time.Second)
		defer stop()
		return httpServer.Shutdown(shutdownContext)
	case <-ctx.Done():
		return ctx.Err()
	}
}

type Config struct {
	Database struct {
		DataSourceName     string
		OnlyMakeMigrations bool
	}
	Telegram struct {
		Token      string
		AdminID    int64
		GroupID    int64
		InviteLink string
	}
	trustedProxy string
	noAuth       bool
	domain       string
}

func loadConfig() (*Config, error) {
	err := os.Setenv("XDG_CACHE_HOME", "/root/.cache")
	if err != nil {
		return nil, err
	}
	// Loading config
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	onlyMakeMigrations := os.Getenv("ONLY_MAKE_MIGRATIONS") == "true"
	botToken := os.Getenv("BOT_TOKEN")
	noAuth := os.Getenv("NO_AUTH") == "true"
	trustedProxy := os.Getenv("TRUSTED_PROXY")
	domain := os.Getenv("DOMAIN")

	adminID, err := strconv.ParseInt(os.Getenv("ADMIN_ID"), 10, 64)
	if err != nil {
		return nil, err
	}
	groupID, err := strconv.ParseInt(os.Getenv("GROUP_ID"), 10, 64)
	if err != nil {
		return nil, err
	}
	inviteLink := os.Getenv("INVITE_LINK")

	return &Config{
		Database: struct {
			DataSourceName     string
			OnlyMakeMigrations bool
		}{
			DataSourceName:     databaseURL,
			OnlyMakeMigrations: onlyMakeMigrations,
		},
		Telegram: struct {
			Token      string
			AdminID    int64
			GroupID    int64
			InviteLink string
		}{
			Token:      botToken,
			AdminID:    adminID,
			GroupID:    groupID,
			InviteLink: inviteLink,
		},
		trustedProxy: trustedProxy,
		noAuth:       noAuth,
		domain:       domain,
	}, nil
}
