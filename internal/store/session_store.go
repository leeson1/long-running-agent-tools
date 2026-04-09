package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/leeson1/agent-forge/internal/session"
)

// SessionStore 会话持久化存储
type SessionStore struct {
	baseDir string
}

// NewSessionStore 创建 SessionStore 实例
func NewSessionStore(baseDir string) *SessionStore {
	return &SessionStore{baseDir: baseDir}
}

// sessionsDir 返回指定任务的 sessions 目录
func (s *SessionStore) sessionsDir(taskID string) string {
	return filepath.Join(s.baseDir, tasksDirName, taskID, sessionsDirName)
}

// sessionFile 返回 session JSON 文件路径
func (s *SessionStore) sessionFile(taskID, sessionID string) string {
	return filepath.Join(s.sessionsDir(taskID), sessionID+".json")
}

// logFile 返回 session log 文件路径
func (s *SessionStore) logFile(taskID, sessionID string) string {
	return filepath.Join(s.sessionsDir(taskID), sessionID+".log")
}

// Save 保存会话记录
func (s *SessionStore) Save(sess *session.Session) error {
	dir := s.sessionsDir(sess.TaskID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create sessions dir: %w", err)
	}

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	path := s.sessionFile(sess.TaskID, sess.ID)
	return os.WriteFile(path, data, 0644)
}

// Get 获取会话记录
func (s *SessionStore) Get(taskID, sessionID string) (*session.Session, error) {
	path := s.sessionFile(taskID, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read session %s: %w", sessionID, err)
	}

	var sess session.Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}
	return &sess, nil
}

// List 列出指定任务的所有会话
func (s *SessionStore) List(taskID string) ([]*session.Session, error) {
	dir := s.sessionsDir(taskID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []*session.Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".json")
		sess, err := s.Get(taskID, sessionID)
		if err != nil {
			continue
		}
		sessions = append(sessions, sess)
	}

	// 按启动时间排序
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.Before(sessions[j].StartedAt)
	})

	return sessions, nil
}
