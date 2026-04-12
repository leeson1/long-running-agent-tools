package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/leeson1/agent-forge/internal/session"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/stream"
	"github.com/leeson1/agent-forge/internal/task"
)

func setupTestServer(t *testing.T) *Server {
	t.Helper()
	baseDir := t.TempDir()
	eb := stream.NewEventBus(64)
	ts := store.NewTaskStore(baseDir)
	ss := store.NewSessionStore(baseDir)
	ls := store.NewLogStore(baseDir)
	exec := session.NewExecutor(baseDir, session.DefaultExecutorConfig())
	return NewServer(eb, ts, ss, ls, exec)
}

func TestHealthCheck(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("Status: got %s, want ok", resp["status"])
	}
}

func TestCreateTask(t *testing.T) {
	s := setupTestServer(t)

	body := `{"name":"Test Task","description":"A test","template":"default","config":{"max_parallel_workers":2,"session_timeout":"10m","workspace_dir":"/tmp/test"}}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Status: got %d, want %d. Body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp task.Task
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Name != "Test Task" {
		t.Errorf("Name: got %s, want Test Task", resp.Name)
	}
	if resp.Status != task.StatusPending {
		t.Errorf("Status: got %s, want pending", resp.Status)
	}
}

func TestCreateTask_MissingName(t *testing.T) {
	s := setupTestServer(t)

	body := `{"description":"no name","config":{"workspace_dir":"/tmp"}}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCreateTask_MissingWorkspace(t *testing.T) {
	s := setupTestServer(t)

	body := `{"name":"Test"}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestListTasks_Empty(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp []task.Task
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 0 {
		t.Errorf("Tasks count: got %d, want 0", len(resp))
	}
}

func TestCreateAndGetTask(t *testing.T) {
	s := setupTestServer(t)

	// 创建
	body := `{"name":"My Task","description":"test","config":{"workspace_dir":"/tmp/ws"}}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	var created task.Task
	json.NewDecoder(w.Body).Decode(&created)

	// 获取
	req = httptest.NewRequest("GET", "/api/tasks/"+created.ID, nil)
	w = httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Get status: got %d, want %d", w.Code, http.StatusOK)
	}

	var fetched task.Task
	json.NewDecoder(w.Body).Decode(&fetched)
	if fetched.ID != created.ID {
		t.Errorf("ID mismatch: got %s, want %s", fetched.ID, created.ID)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/tasks/nonexistent", nil)
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestUpdateTask(t *testing.T) {
	s := setupTestServer(t)

	// 创建
	body := `{"name":"Original","description":"desc","config":{"workspace_dir":"/tmp"}}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	var created task.Task
	json.NewDecoder(w.Body).Decode(&created)

	// 更新
	updateBody := `{"name":"Updated Name"}`
	req = httptest.NewRequest("PUT", "/api/tasks/"+created.ID, bytes.NewBufferString(updateBody))
	w = httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Update status: got %d, want %d", w.Code, http.StatusOK)
	}

	var updated task.Task
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Name != "Updated Name" {
		t.Errorf("Name: got %s, want Updated Name", updated.Name)
	}
}

func TestDeleteTask(t *testing.T) {
	s := setupTestServer(t)

	// 创建
	body := `{"name":"ToDelete","config":{"workspace_dir":"/tmp"}}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	var created task.Task
	json.NewDecoder(w.Body).Decode(&created)

	// 删除
	req = httptest.NewRequest("DELETE", "/api/tasks/"+created.ID, nil)
	w = httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Delete status: got %d, want %d", w.Code, http.StatusOK)
	}

	// 确认删除
	req = httptest.NewRequest("GET", "/api/tasks/"+created.ID, nil)
	w = httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("After delete, Get status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestListTasks_WithFilter(t *testing.T) {
	s := setupTestServer(t)

	// 创建两个任务
	for _, name := range []string{"Task A", "Task B"} {
		body := `{"name":"` + name + `","config":{"workspace_dir":"/tmp"}}`
		req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, req)
	}

	// 列出所有
	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	var tasks []task.Task
	json.NewDecoder(w.Body).Decode(&tasks)
	if len(tasks) != 2 {
		t.Errorf("Tasks count: got %d, want 2", len(tasks))
	}

	// 按 pending 筛选
	req = httptest.NewRequest("GET", "/api/tasks?status=pending", nil)
	w = httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	json.NewDecoder(w.Body).Decode(&tasks)
	if len(tasks) != 2 {
		t.Errorf("Filtered tasks count: got %d, want 2", len(tasks))
	}
}

func TestStartTask(t *testing.T) {
	s := setupTestServer(t)

	workDir := t.TempDir()
	body := `{"name":"Startable","config":{"workspace_dir":"` + workDir + `"}}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	var created task.Task
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest("POST", "/api/tasks/"+created.ID+"/start", nil)
	w = httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Start status: got %d, want %d", w.Code, http.StatusOK)
	}

	// 等后台 pipeline goroutine 结束（它会因为找不到 claude CLI 快速失败）
	time.Sleep(500 * time.Millisecond)
}

func TestListSessions_Empty(t *testing.T) {
	s := setupTestServer(t)

	body := `{"name":"Task","config":{"workspace_dir":"/tmp"}}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	var created task.Task
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest("GET", "/api/tasks/"+created.ID+"/sessions", nil)
	w = httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status: got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestGetEvents_Empty(t *testing.T) {
	s := setupTestServer(t)

	body := `{"name":"Task","config":{"workspace_dir":"/tmp"}}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	var created task.Task
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest("GET", "/api/tasks/"+created.ID+"/events", nil)
	w = httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status: got %d, want %d", w.Code, http.StatusOK)
	}
}
