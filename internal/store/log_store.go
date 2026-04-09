package store

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LogStore 日志持久化存储
type LogStore struct {
	baseDir string
}

// NewLogStore 创建 LogStore 实例
func NewLogStore(baseDir string) *LogStore {
	return &LogStore{baseDir: baseDir}
}

// logFilePath 返回日志文件路径
func (s *LogStore) logFilePath(taskID, sessionID string) string {
	return filepath.Join(s.baseDir, tasksDirName, taskID, sessionsDirName, sessionID+".log")
}

// eventsFilePath 返回事件日志文件路径
func (s *LogStore) eventsFilePath(taskID string) string {
	return filepath.Join(s.baseDir, tasksDirName, taskID, eventsDirName, "events.jsonl")
}

// Append 追加日志内容到 session 日志文件
func (s *LogStore) Append(taskID, sessionID, content string) error {
	path := s.logFilePath(taskID, sessionID)

	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create log dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("failed to write log: %w", err)
	}
	return nil
}

// Read 读取 session 日志文件全部内容
func (s *LogStore) Read(taskID, sessionID string) (string, error) {
	path := s.logFilePath(taskID, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read log: %w", err)
	}
	return string(data), nil
}

// Tail 读取日志文件最后 N 行
func (s *LogStore) Tail(taskID, sessionID string, lines int) ([]string, error) {
	path := s.logFilePath(taskID, sessionID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open log: %w", err)
	}
	defer f.Close()

	// 读取所有行
	var allLines []string
	scanner := bufio.NewScanner(f)
	// 增大 buffer 以处理长行
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan log: %w", err)
	}

	// 取最后 N 行
	if lines >= len(allLines) {
		return allLines, nil
	}
	return allLines[len(allLines)-lines:], nil
}

// ReadFrom 从指定偏移量开始读取日志（用于增量读取）
func (s *LogStore) ReadFrom(taskID, sessionID string, offset int64) (string, int64, error) {
	path := s.logFilePath(taskID, sessionID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, nil
		}
		return "", 0, err
	}
	defer f.Close()

	// 跳到偏移位置
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return "", offset, err
		}
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return "", offset, err
	}

	newOffset := offset + int64(len(data))
	return string(data), newOffset, nil
}

// AppendEvent 追加事件到 events.jsonl
func (s *LogStore) AppendEvent(taskID, eventJSON string) error {
	path := s.eventsFilePath(taskID)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create events dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open events file: %w", err)
	}
	defer f.Close()

	line := strings.TrimRight(eventJSON, "\n") + "\n"
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}
	return nil
}

// ReadEvents 读取所有事件
func (s *LogStore) ReadEvents(taskID string) ([]string, error) {
	path := s.eventsFilePath(taskID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	return lines, nil
}
