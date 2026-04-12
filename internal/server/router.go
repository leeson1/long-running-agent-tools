package server

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/leeson1/agent-forge/internal/session"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/stream"
)

// Server HTTP 服务器
type Server struct {
	router       chi.Router
	hub          *WSHub
	eventBus     *stream.EventBus
	taskStore    *store.TaskStore
	sessionStore *store.SessionStore
	logStore     *store.LogStore
	executor     *session.Executor
	pipeline     *Pipeline
}

// NewServer 创建 HTTP 服务器
func NewServer(
	eventBus *stream.EventBus,
	taskStore *store.TaskStore,
	sessionStore *store.SessionStore,
	logStore *store.LogStore,
	executor *session.Executor,
) *Server {
	hub := NewWSHub(eventBus)
	go hub.Run()

	s := &Server{
		hub:          hub,
		eventBus:     eventBus,
		taskStore:    taskStore,
		sessionStore: sessionStore,
		logStore:     logStore,
		executor:     executor,
	}
	s.pipeline = NewPipeline(executor, taskStore, sessionStore, logStore, eventBus)
	s.router = s.setupRouter()
	return s
}

// Router 返回 chi.Router（用于测试）
func (s *Server) Router() chi.Router {
	return s.router
}

// ServeHTTP 实现 http.Handler
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// setupRouter 设置路由
func (s *Server) setupRouter() chi.Router {
	r := chi.NewRouter()

	// 中间件
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// API 路由
	r.Route("/api", func(r chi.Router) {
		// 任务
		r.Route("/tasks", func(r chi.Router) {
			r.Get("/", s.ListTasks)
			r.Post("/", s.CreateTask)

			r.Route("/{taskID}", func(r chi.Router) {
				r.Get("/", s.GetTask)
				r.Put("/", s.UpdateTask)
				r.Delete("/", s.DeleteTask)

				// 任务控制
				r.Post("/start", s.StartTask)
				r.Post("/stop", s.StopTask)

				// Sessions
				r.Get("/sessions", s.ListSessions)
				r.Get("/sessions/{sessionID}", s.GetSession)

				// Features
				r.Get("/features", s.GetFeatures)

				// Logs
				r.Get("/logs/{sessionID}", s.GetLogs)

				// Events
				r.Get("/events", s.GetEvents)

				// Intervention（人工干预）
				r.Post("/intervene", s.Intervene)
			})
		})

		// WebSocket
		r.Get("/ws", s.hub.ServeWS)

		// 健康检查
		r.Get("/health", s.HealthCheck)
	})

	// 前端静态文件服务（SPA fallback）
	staticDir := findStaticDir()
	if staticDir != "" {
		fileServer := http.FileServer(http.Dir(staticDir))
		r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
			// 如果文件存在，直接 serve
			path := filepath.Join(staticDir, strings.TrimPrefix(req.URL.Path, "/"))
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, req)
				return
			}
			// SPA fallback: 返回 index.html
			http.ServeFile(w, req, filepath.Join(staticDir, "index.html"))
		})
	}

	return r
}

// findStaticDir 查找前端构建产出目录
func findStaticDir() string {
	candidates := []string{
		"/app/web/dist",       // Docker 容器
		"web/dist",            // 本地开发（项目根目录运行）
		"../web/dist",         // 本地开发（从 cmd/ 运行）
	}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			if _, err := fs.Stat(os.DirFS(dir), "index.html"); err == nil {
				return dir
			}
		}
	}
	return ""
}
