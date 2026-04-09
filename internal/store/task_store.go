package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/leeson1/agent-forge/internal/task"
)

const (
	taskFileName        = "task.json"
	featureListFileName = "feature_list.json"
	executionPlanFileName = "execution_plan.json"
	progressFileName    = "progress.txt"
	sessionsDirName     = "sessions"
	eventsDirName       = "events"
	promptsDirName      = "prompts"
)

// TaskStore 任务持久化存储
type TaskStore struct {
	baseDir string
}

// NewTaskStore 创建 TaskStore 实例
func NewTaskStore(baseDir string) *TaskStore {
	return &TaskStore{baseDir: baseDir}
}

// taskDir 返回指定任务的目录
func (s *TaskStore) taskDir(taskID string) string {
	return filepath.Join(s.baseDir, tasksDirName, taskID)
}

// Create 创建任务（创建目录结构 + 写入 task.json）
func (s *TaskStore) Create(t *task.Task) error {
	dir := s.taskDir(t.ID)

	// 检查是否已存在
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("task already exists: %s", t.ID)
	}

	// 创建子目录结构
	subdirs := []string{
		dir,
		filepath.Join(dir, sessionsDirName),
		filepath.Join(dir, eventsDirName),
		filepath.Join(dir, promptsDirName),
	}
	for _, d := range subdirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", d, err)
		}
	}

	// 写入 task.json
	return s.writeJSON(filepath.Join(dir, taskFileName), t)
}

// Get 获取任务
func (s *TaskStore) Get(taskID string) (*task.Task, error) {
	path := filepath.Join(s.taskDir(taskID), taskFileName)
	var t task.Task
	if err := s.readJSON(path, &t); err != nil {
		return nil, fmt.Errorf("failed to read task %s: %w", taskID, err)
	}
	return &t, nil
}

// Update 更新任务
func (s *TaskStore) Update(t *task.Task) error {
	dir := s.taskDir(t.ID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("task not found: %s", t.ID)
	}
	return s.writeJSON(filepath.Join(dir, taskFileName), t)
}

// Delete 删除任务（删除整个任务目录）
func (s *TaskStore) Delete(taskID string) error {
	dir := s.taskDir(taskID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("task not found: %s", taskID)
	}
	return os.RemoveAll(dir)
}

// List 列出所有任务，支持按状态筛选
func (s *TaskStore) List(statusFilter *task.TaskStatus) ([]*task.Task, error) {
	tasksDir := filepath.Join(s.baseDir, tasksDirName)
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var tasks []*task.Task
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t, err := s.Get(entry.Name())
		if err != nil {
			continue // 跳过无法读取的任务
		}
		if statusFilter != nil && t.Status != *statusFilter {
			continue
		}
		tasks = append(tasks, t)
	}

	// 按创建时间倒序
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})

	return tasks, nil
}

// SaveFeatureList 保存功能清单
func (s *TaskStore) SaveFeatureList(taskID string, fl *task.FeatureList) error {
	path := filepath.Join(s.taskDir(taskID), featureListFileName)
	return s.writeJSON(path, fl)
}

// GetFeatureList 获取功能清单
func (s *TaskStore) GetFeatureList(taskID string) (*task.FeatureList, error) {
	path := filepath.Join(s.taskDir(taskID), featureListFileName)
	var fl task.FeatureList
	if err := s.readJSON(path, &fl); err != nil {
		return nil, err
	}
	return &fl, nil
}

// SaveExecutionPlan 保存执行计划
func (s *TaskStore) SaveExecutionPlan(taskID string, ep *task.ExecutionPlan) error {
	path := filepath.Join(s.taskDir(taskID), executionPlanFileName)
	return s.writeJSON(path, ep)
}

// GetExecutionPlan 获取执行计划
func (s *TaskStore) GetExecutionPlan(taskID string) (*task.ExecutionPlan, error) {
	path := filepath.Join(s.taskDir(taskID), executionPlanFileName)
	var ep task.ExecutionPlan
	if err := s.readJSON(path, &ep); err != nil {
		return nil, err
	}
	return &ep, nil
}

// SaveProgress 保存进度文件
func (s *TaskStore) SaveProgress(taskID string, content string) error {
	path := filepath.Join(s.taskDir(taskID), progressFileName)
	return os.WriteFile(path, []byte(content), 0644)
}

// GetProgress 获取进度文件内容
func (s *TaskStore) GetProgress(taskID string) (string, error) {
	path := filepath.Join(s.taskDir(taskID), progressFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// writeJSON 将对象序列化为 JSON 并写入文件
func (s *TaskStore) writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// readJSON 从文件读取 JSON 并反序列化
func (s *TaskStore) readJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
