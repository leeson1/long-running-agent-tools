package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/leeson1/agent-forge/internal/config"
	"github.com/leeson1/agent-forge/internal/server"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/stream"
	"github.com/leeson1/agent-forge/internal/task"
	"github.com/leeson1/agent-forge/internal/template"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

// ANSI 颜色
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[37m"
	colorBold   = "\033[1m"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "agent-forge",
		Short: "AgentForge - Long-running Agent management system",
		Long: fmt.Sprintf(`%s🔨 AgentForge v%s%s
A universal framework for long-running AI agent tasks powered by Claude Code CLI.
Manage parallel agents, templates, and real-time monitoring.`, colorBold, version, colorReset),
		Version: version,
	}

	rootCmd.AddCommand(
		newServeCmd(),
		newInitCmd(),
		newRunCmd(),
		newStopCmd(),
		newStatusCmd(),
		newLogsCmd(),
		newTemplateCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ==================== serve ====================

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the AgentForge HTTP server + WebSocket",
		RunE:  runServe,
	}
	cmd.Flags().IntP("port", "p", 0, "Server port (default: from config or 8080)")
	cmd.Flags().String("host", "", "Server host (default: from config or 0.0.0.0)")
	return cmd
}

func runServe(cmd *cobra.Command, args []string) error {
	// 初始化存储目录
	if err := store.Init(); err != nil {
		return fmt.Errorf("init storage: %w", err)
	}

	// 加载配置
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 命令行参数覆盖
	if p, _ := cmd.Flags().GetInt("port"); p > 0 {
		cfg.Server.Port = p
	}
	if h, _ := cmd.Flags().GetString("host"); h != "" {
		cfg.Server.Host = h
	}

	// 创建核心组件
	baseDir := store.BaseDir()
	taskStore := store.NewTaskStore(baseDir)
	sessionStore := store.NewSessionStore(baseDir)
	logStore := store.NewLogStore(baseDir)
	eventBus := stream.NewEventBus(100)

	srv := server.NewServer(eventBus, taskStore, sessionStore, logStore)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	fmt.Printf("\n%s🔨 AgentForge v%s%s\n", colorBold, version, colorReset)
	fmt.Printf("   %sServer:%s  http://%s\n", colorCyan, colorReset, addr)
	fmt.Printf("   %sWebSocket:%s ws://%s/api/ws\n", colorCyan, colorReset, addr)
	fmt.Printf("   %sStorage:%s  %s\n", colorCyan, colorReset, baseDir)
	fmt.Printf("   %sPress Ctrl+C to stop%s\n\n", colorGray, colorReset)

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: srv,
	}

	// 优雅关闭
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "%sServer error: %v%s\n", colorRed, err, colorReset)
			os.Exit(1)
		}
	}()

	<-done
	fmt.Printf("\n%s⏹  Shutting down...%s\n", colorYellow, colorReset)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpSrv.Shutdown(ctx)

	fmt.Printf("%s✅ Server stopped%s\n", colorGreen, colorReset)
	return nil
}

// ==================== init ====================

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init <project-dir>",
		Short: "Initialize a new agent task for a project",
		Args:  cobra.ExactArgs(1),
		RunE:  runInit,
	}
	cmd.Flags().StringP("name", "n", "", "Task name")
	cmd.Flags().StringP("description", "d", "", "Task description")
	cmd.Flags().StringP("template", "t", "default", "Template to use")
	cmd.Flags().IntP("workers", "w", 2, "Max parallel workers")
	cmd.Flags().String("timeout", "30m", "Session timeout")
	return cmd
}

func runInit(cmd *cobra.Command, args []string) error {
	projectDir := args[0]

	// 检查目录
	info, err := os.Stat(projectDir)
	if err != nil {
		return fmt.Errorf("project directory %q: %w", projectDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", projectDir)
	}

	// 初始化存储
	if err := store.Init(); err != nil {
		return fmt.Errorf("init storage: %w", err)
	}

	name, _ := cmd.Flags().GetString("name")
	description, _ := cmd.Flags().GetString("description")
	tmplName, _ := cmd.Flags().GetString("template")
	workers, _ := cmd.Flags().GetInt("workers")
	timeout, _ := cmd.Flags().GetString("timeout")

	if name == "" {
		name = info.Name()
	}

	taskStore := store.NewTaskStore(store.BaseDir())

	// 生成唯一 ID
	taskID := fmt.Sprintf("task-%d", time.Now().UnixNano())

	taskConfig := task.TaskConfig{
		MaxParallelWorkers: workers,
		SessionTimeout:     timeout,
		WorkspaceDir:       projectDir,
	}

	t := task.NewTask(taskID, name, description, tmplName, taskConfig)

	if err := taskStore.Create(t); err != nil {
		return fmt.Errorf("create task: %w", err)
	}

	fmt.Printf("\n%s🚀 Task Created%s\n", colorBold, colorReset)
	fmt.Printf("   %sID:%s       %s\n", colorCyan, colorReset, t.ID)
	fmt.Printf("   %sName:%s     %s\n", colorCyan, colorReset, t.Name)
	fmt.Printf("   %sTemplate:%s %s\n", colorCyan, colorReset, tmplName)
	fmt.Printf("   %sWorkers:%s  %d\n", colorCyan, colorReset, workers)
	fmt.Printf("   %sDir:%s      %s\n", colorCyan, colorReset, projectDir)
	fmt.Printf("\n   Run: %sagent-forge run %s%s\n\n", colorGreen, t.ID, colorReset)

	return nil
}

// ==================== run ====================

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <task-id>",
		Short: "Start a task",
		Args:  cobra.ExactArgs(1),
		RunE:  runRun,
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output after starting")
	return cmd
}

func runRun(cmd *cobra.Command, args []string) error {
	taskID := args[0]
	taskStore := store.NewTaskStore(store.BaseDir())

	t, err := taskStore.Get(taskID)
	if err != nil {
		return fmt.Errorf("task %q not found: %w", taskID, err)
	}

	fmt.Printf("%s▶️  Starting task: %s (%s)%s\n", colorGreen, t.Name, shortID(t.ID), colorReset)
	fmt.Printf("   Status: %s → running\n", t.Status)

	// 更新状态
	if err := t.TransitionTo(task.StatusRunning); err != nil {
		// 如果不能直接到 running，尝试先初始化
		if err2 := t.TransitionTo(task.StatusInitializing); err2 != nil {
			return fmt.Errorf("cannot start task (current: %s): %v", t.Status, err)
		}
	}
	if err := taskStore.Update(t); err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	fmt.Printf("   %s✅ Task started%s\n", colorGreen, colorReset)

	follow, _ := cmd.Flags().GetBool("follow")
	if follow {
		fmt.Printf("\n   %s[Following logs - Press Ctrl+C to stop]%s\n\n", colorGray, colorReset)
		fmt.Printf("   Waiting for agent output...\n")
	}

	return nil
}

// ==================== stop ====================

func newStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop <task-id>",
		Short: "Stop a running task",
		Args:  cobra.ExactArgs(1),
		RunE:  runStop,
	}
	cmd.Flags().Bool("force", false, "Force stop without waiting")
	return cmd
}

func runStop(cmd *cobra.Command, args []string) error {
	taskID := args[0]
	taskStore := store.NewTaskStore(store.BaseDir())
	force, _ := cmd.Flags().GetBool("force")

	t, err := taskStore.Get(taskID)
	if err != nil {
		return fmt.Errorf("task %q not found: %w", taskID, err)
	}

	if force {
		fmt.Printf("%s⏹  Force stopping task: %s%s\n", colorRed, t.Name, colorReset)
	} else {
		fmt.Printf("%s⏹  Stopping task: %s (waiting for current workers)%s\n", colorYellow, t.Name, colorReset)
	}

	if err := t.TransitionTo(task.StatusCancelled); err != nil {
		// 直接强制设置状态
		t.Status = task.StatusCancelled
		t.UpdatedAt = time.Now()
	}
	if err := taskStore.Update(t); err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	fmt.Printf("   %s✅ Task stopped%s\n", colorGreen, colorReset)
	return nil
}

// ==================== status ====================

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show all task statuses",
		RunE:  runStatus,
	}
}

func runStatus(cmd *cobra.Command, args []string) error {
	taskStore := store.NewTaskStore(store.BaseDir())
	tasks, err := taskStore.List(nil)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Printf("\n%s📋 No tasks found%s\n", colorYellow, colorReset)
		fmt.Printf("   Use '%sagent-forge init <project-dir>%s' to create one.\n\n", colorCyan, colorReset)
		return nil
	}

	fmt.Printf("\n%s📋 AgentForge Tasks%s (%d total)\n\n", colorBold, colorReset, len(tasks))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "  %sID\tNAME\tSTATUS\tPROGRESS\tBATCH\tTEMPLATE%s\n",
		colorGray, colorReset)
	fmt.Fprintf(w, "  %s──\t────\t──────\t────────\t─────\t────────%s\n",
		colorGray, colorReset)

	for _, t := range tasks {
		sc := statusColorCode(string(t.Status))
		progress := "N/A"
		if t.Progress.FeaturesTotal > 0 {
			pct := float64(t.Progress.FeaturesCompleted) / float64(t.Progress.FeaturesTotal) * 100
			progress = fmt.Sprintf("%d/%d (%.0f%%)", t.Progress.FeaturesCompleted, t.Progress.FeaturesTotal, pct)
		}
		batchInfo := "N/A"
		if t.Progress.TotalBatches > 0 {
			batchInfo = fmt.Sprintf("%d/%d", t.Progress.CurrentBatch, t.Progress.TotalBatches)
		}

		fmt.Fprintf(w, "  %s\t%s\t%s%s%s\t%s\t%s\t%s\n",
			shortID(t.ID),
			truncate(t.Name, 20),
			sc, t.Status, colorReset,
			progress,
			batchInfo,
			t.Template,
		)
	}
	w.Flush()
	fmt.Println()

	return nil
}

// ==================== logs ====================

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs <task-id>",
		Short: "View task logs",
		Args:  cobra.ExactArgs(1),
		RunE:  runLogs,
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")
	cmd.Flags().Int("session", 0, "Show logs for specific session number")
	cmd.Flags().String("level", "", "Filter by log level (info/warn/error)")
	cmd.Flags().IntP("tail", "n", 50, "Number of recent lines to show")
	return cmd
}

func runLogs(cmd *cobra.Command, args []string) error {
	taskID := args[0]
	baseDir := store.BaseDir()
	taskStore := store.NewTaskStore(baseDir)
	sessionStore := store.NewSessionStore(baseDir)
	tail, _ := cmd.Flags().GetInt("tail")

	t, err := taskStore.Get(taskID)
	if err != nil {
		return fmt.Errorf("task %q not found: %w", taskID, err)
	}

	fmt.Printf("%s📜 Logs for: %s (%s)%s\n\n", colorBold, t.Name, shortID(t.ID), colorReset)

	// 查找 Session 日志
	sessions, err := sessionStore.List(taskID)
	if err != nil || len(sessions) == 0 {
		fmt.Printf("   %sNo sessions found%s\n", colorGray, colorReset)
		return nil
	}

	logStore := store.NewLogStore(baseDir)

	for _, sess := range sessions {
		content, err := logStore.Read(taskID, sess.ID)
		if err != nil {
			continue
		}

		lines := strings.Split(content, "\n")
		if len(lines) > tail {
			lines = lines[len(lines)-tail:]
		}

		fmt.Printf("  %s── Session: %s (%s) ──%s\n", colorCyan, shortID(sess.ID), sess.Type, colorReset)
		for _, line := range lines {
			if line == "" {
				continue
			}
			printColoredLog(line)
		}
		fmt.Println()
	}

	return nil
}

// ==================== template ====================

func newTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "Manage project templates",
	}
	cmd.AddCommand(newTemplateListCmd())
	return cmd
}

func newTemplateListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available templates",
		RunE:  runTemplateList,
	}
}

func runTemplateList(cmd *cobra.Command, args []string) error {
	reg, err := template.NewRegistryWithBuiltins()
	if err != nil {
		return fmt.Errorf("load templates: %w", err)
	}

	templates := reg.List()

	fmt.Printf("\n%s📦 Available Templates%s (%d total)\n\n", colorBold, colorReset, len(templates))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "  %sID\tNAME\tCATEGORY\tDESCRIPTION%s\n", colorGray, colorReset)
	fmt.Fprintf(w, "  %s──\t────\t────────\t───────────%s\n", colorGray, colorReset)

	for _, tmpl := range templates {
		fmt.Fprintf(w, "  %s%s%s\t%s\t%s\t%s\n",
			colorCyan, tmpl.Config.ID, colorReset,
			tmpl.Config.Name,
			tmpl.Config.Category,
			truncate(tmpl.Config.Description, 50),
		)
	}
	w.Flush()
	fmt.Println()

	return nil
}

// ==================== helpers ====================

func statusColorCode(status string) string {
	switch status {
	case "running", "initializing", "planning", "merging", "validating":
		return colorGreen
	case "completed":
		return colorGreen + colorBold
	case "failed":
		return colorRed
	case "pending", "paused":
		return colorYellow
	case "cancelled":
		return colorGray
	default:
		return ""
	}
}

func shortID(id string) string {
	if len(id) > 16 {
		return id[:16] + "..."
	}
	return id
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func printColoredLog(line string) {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "[error]") || strings.Contains(lower, "error:"):
		fmt.Printf("  %s%s%s\n", colorRed, line, colorReset)
	case strings.Contains(lower, "[warn]") || strings.Contains(lower, "warning:"):
		fmt.Printf("  %s%s%s\n", colorYellow, line, colorReset)
	case strings.Contains(lower, "[pass]"):
		fmt.Printf("  %s%s%s\n", colorGreen, line, colorReset)
	default:
		fmt.Printf("  %s\n", line)
	}
}
