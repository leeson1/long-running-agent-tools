package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "0.1.0"

func main() {
	rootCmd := &cobra.Command{
		Use:   "agent-forge",
		Short: "AgentForge - Long-running Agent management system",
		Long: `AgentForge 是一个通用的长时间 Agent 运行框架 + Web UI 管理平台。
基于 Claude Code CLI，通过模板/插件机制适配不同类型的复杂项目。`,
		Version: version,
	}

	// serve 命令 - 启动 HTTP 服务
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the AgentForge server",
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Flags().GetInt("port")
			host, _ := cmd.Flags().GetString("host")
			fmt.Printf("🔨 AgentForge v%s\n", version)
			fmt.Printf("   Server starting on http://%s:%d\n", host, port)
			// TODO: Phase 8 实现 HTTP 服务器
			return nil
		},
	}
	serveCmd.Flags().IntP("port", "p", 8080, "Server port")
	serveCmd.Flags().String("host", "0.0.0.0", "Server host")

	// status 命令 - 查看任务状态
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show all task statuses",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("📋 AgentForge Tasks")
			fmt.Println("   No tasks found. Use 'agent-forge init' to create one.")
			// TODO: Phase 14 完善
			return nil
		},
	}

	// init 命令 - 创建任务
	initCmd := &cobra.Command{
		Use:   "init [project-dir]",
		Short: "Initialize a new agent task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("🚀 Initializing task in: %s\n", args[0])
			// TODO: Phase 14 完善
			return nil
		},
	}

	// run 命令 - 启动任务
	runCmd := &cobra.Command{
		Use:   "run [task-id]",
		Short: "Start a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("▶️  Starting task: %s\n", args[0])
			// TODO: Phase 14 完善
			return nil
		},
	}

	// stop 命令 - 停止任务
	stopCmd := &cobra.Command{
		Use:   "stop [task-id]",
		Short: "Stop a running task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("⏹  Stopping task: %s\n", args[0])
			// TODO: Phase 14 完善
			return nil
		},
	}

	// logs 命令 - 查看日志
	logsCmd := &cobra.Command{
		Use:   "logs [task-id]",
		Short: "View task logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("📜 Logs for task: %s\n", args[0])
			// TODO: Phase 14 完善
			return nil
		},
	}
	logsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	logsCmd.Flags().Int("session", 0, "Show logs for specific session")

	// template 命令组
	templateCmd := &cobra.Command{
		Use:   "template",
		Short: "Manage project templates",
	}
	templateListCmd := &cobra.Command{
		Use:   "list",
		Short: "List available templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("📦 Available Templates")
			fmt.Println("   fullstack-web    - Full-stack Web Application")
			fmt.Println("   cli-tool         - CLI Tool")
			fmt.Println("   data-analysis    - Data Analysis Project")
			// TODO: Phase 14 完善
			return nil
		},
	}
	templateCmd.AddCommand(templateListCmd)

	rootCmd.AddCommand(serveCmd, statusCmd, initCmd, runCmd, stopCmd, logsCmd, templateCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
