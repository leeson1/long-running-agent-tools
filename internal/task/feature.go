package task

// Feature 功能条目
type Feature struct {
	ID          string   `json:"id"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
	DependsOn   []string `json:"depends_on"`
	Batch       *int     `json:"batch"`  // Coordinator 分配的批次号（nil 表示未分配）
	Passes      bool     `json:"passes"` // 是否通过验证
}

// FeatureList 功能清单
type FeatureList struct {
	Features []Feature `json:"features"`
}

// GetByID 根据 ID 获取 Feature
func (fl *FeatureList) GetByID(id string) *Feature {
	for i := range fl.Features {
		if fl.Features[i].ID == id {
			return &fl.Features[i]
		}
	}
	return nil
}

// PendingFeatures 返回未完成的 features
func (fl *FeatureList) PendingFeatures() []Feature {
	var pending []Feature
	for _, f := range fl.Features {
		if !f.Passes {
			pending = append(pending, f)
		}
	}
	return pending
}

// CompletedFeatures 返回已完成的 features
func (fl *FeatureList) CompletedFeatures() []Feature {
	var completed []Feature
	for _, f := range fl.Features {
		if f.Passes {
			completed = append(completed, f)
		}
	}
	return completed
}

// CompletionRate 返回完成率 (0.0 ~ 1.0)
func (fl *FeatureList) CompletionRate() float64 {
	if len(fl.Features) == 0 {
		return 0
	}
	return float64(len(fl.CompletedFeatures())) / float64(len(fl.Features))
}

// FeaturesInBatch 返回指定 Batch 中的 features
func (fl *FeatureList) FeaturesInBatch(batch int) []Feature {
	var result []Feature
	for _, f := range fl.Features {
		if f.Batch != nil && *f.Batch == batch {
			result = append(result, f)
		}
	}
	return result
}

// Validate 校验 FeatureList 合法性
func (fl *FeatureList) Validate() error {
	ids := make(map[string]bool)

	// 检查 ID 唯一性
	for _, f := range fl.Features {
		if f.ID == "" {
			return &FeatureValidationError{Message: "feature ID cannot be empty"}
		}
		if ids[f.ID] {
			return &FeatureValidationError{Message: "duplicate feature ID: " + f.ID}
		}
		ids[f.ID] = true
	}

	// 检查依赖引用有效性
	for _, f := range fl.Features {
		for _, dep := range f.DependsOn {
			if !ids[dep] {
				return &FeatureValidationError{
					Message: "feature " + f.ID + " depends on unknown feature: " + dep,
				}
			}
		}
	}

	// 检查循环依赖
	if err := fl.detectCycle(); err != nil {
		return err
	}

	return nil
}

// detectCycle 检测依赖关系是否存在循环
func (fl *FeatureList) detectCycle() error {
	// 构建邻接表
	graph := make(map[string][]string)
	for _, f := range fl.Features {
		graph[f.ID] = f.DependsOn
	}

	// DFS 检测环
	const (
		white = 0 // 未访问
		gray  = 1 // 正在访问（在当前 DFS 路径上）
		black = 2 // 已完成访问
	)

	color := make(map[string]int)
	for _, f := range fl.Features {
		color[f.ID] = white
	}

	var dfs func(id string) error
	dfs = func(id string) error {
		color[id] = gray
		for _, dep := range graph[id] {
			if color[dep] == gray {
				return &FeatureValidationError{
					Message: "circular dependency detected involving: " + id + " and " + dep,
				}
			}
			if color[dep] == white {
				if err := dfs(dep); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}

	for _, f := range fl.Features {
		if color[f.ID] == white {
			if err := dfs(f.ID); err != nil {
				return err
			}
		}
	}

	return nil
}

// FeatureValidationError 功能清单校验错误
type FeatureValidationError struct {
	Message string
}

func (e *FeatureValidationError) Error() string {
	return "feature validation error: " + e.Message
}

// BatchInfo 批次信息
type BatchInfo struct {
	Batch    int      `json:"batch"`
	Features []string `json:"features"`
	Status   string   `json:"status"` // pending/running/completed/failed
}

// ExecutionPlan 执行计划
type ExecutionPlan struct {
	GeneratedAt string      `json:"generated_at"`
	Batches     []BatchInfo `json:"batches"`
}

// GetBatch 获取指定编号的 Batch
func (ep *ExecutionPlan) GetBatch(num int) *BatchInfo {
	for i := range ep.Batches {
		if ep.Batches[i].Batch == num {
			return &ep.Batches[i]
		}
	}
	return nil
}

// TotalBatches 返回 Batch 总数
func (ep *ExecutionPlan) TotalBatches() int {
	return len(ep.Batches)
}
