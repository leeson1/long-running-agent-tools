// Package store 提供基于文件系统的持久化存储层。
//
// 目录结构:
//
//	~/.agent-forge/
//	├── config.json
//	├── tasks/
//	│   ├── {task-id}/
//	│   │   ├── task.json
//	│   │   ├── feature_list.json
//	│   │   ├── progress.txt
//	│   │   ├── execution_plan.json
//	│   │   ├── sessions/
//	│   │   │   ├── session-001.json
//	│   │   │   ├── session-001.log
//	│   │   └── events/
//	│   │       └── events.jsonl
//	│   └── ...
//	└── templates/
package store

import (
	"os"
	"path/filepath"
)

const (
	defaultBaseDir  = ".agent-forge"
	tasksDirName    = "tasks"
	templatesDirName = "templates"
	configFileName  = "config.json"
)

// BaseDir 返回 AgentForge 的根目录路径
// 默认为 ~/.agent-forge/，可通过 AGENT_FORGE_HOME 环境变量覆盖
func BaseDir() string {
	if env := os.Getenv("AGENT_FORGE_HOME"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", defaultBaseDir)
	}
	return filepath.Join(home, defaultBaseDir)
}

// TasksDir 返回任务存储目录
func TasksDir() string {
	return filepath.Join(BaseDir(), tasksDirName)
}

// TaskDir 返回指定任务的目录
func TaskDir(taskID string) string {
	return filepath.Join(TasksDir(), taskID)
}

// TemplatesDir 返回用户自定义模板目录
func TemplatesDir() string {
	return filepath.Join(BaseDir(), templatesDirName)
}

// EnsureDir 确保目录存在
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}

// Init 初始化 AgentForge 存储目录结构
func Init() error {
	dirs := []string{
		BaseDir(),
		TasksDir(),
		TemplatesDir(),
	}
	for _, dir := range dirs {
		if err := EnsureDir(dir); err != nil {
			return err
		}
	}
	return nil
}
