package task

import (
	"fmt"
	"time"
)

// Scheduler 调度器，负责拓扑排序和 Batch 分组
type Scheduler struct{}

// NewScheduler 创建调度器
func NewScheduler() *Scheduler {
	return &Scheduler{}
}

// Schedule 对 FeatureList 执行拓扑排序，生成 ExecutionPlan
// 使用 Kahn 算法：
// 1. 计算每个节点的入度
// 2. 将入度为 0 的节点归入当前 Batch
// 3. 移除这些节点及其出边，更新入度
// 4. 重复直到所有节点处理完毕
// 5. 如果有剩余节点未处理，说明存在循环依赖
func (s *Scheduler) Schedule(fl *FeatureList) (*ExecutionPlan, error) {
	if len(fl.Features) == 0 {
		return nil, fmt.Errorf("feature list is empty")
	}

	// 先校验合法性
	if err := fl.Validate(); err != nil {
		return nil, fmt.Errorf("invalid feature list: %w", err)
	}

	// 构建索引和邻接表
	featureIndex := make(map[string]*Feature)  // id -> Feature
	dependents := make(map[string][]string)     // id -> 依赖它的 feature IDs
	inDegree := make(map[string]int)            // id -> 入度

	for i := range fl.Features {
		f := &fl.Features[i]
		featureIndex[f.ID] = f
		inDegree[f.ID] = len(f.DependsOn)

		for _, dep := range f.DependsOn {
			dependents[dep] = append(dependents[dep], f.ID)
		}
	}

	// Kahn 算法
	var batches []BatchInfo
	processed := 0
	batchNum := 0

	for processed < len(fl.Features) {
		// 收集入度为 0 的节点
		var ready []string
		for _, f := range fl.Features {
			if inDegree[f.ID] == 0 {
				ready = append(ready, f.ID)
			}
		}

		if len(ready) == 0 {
			// 不应该到这里（Validate 已经检测了循环依赖）
			return nil, fmt.Errorf("unexpected: no features with zero in-degree, possible circular dependency")
		}

		// 创建 Batch
		batch := BatchInfo{
			Batch:    batchNum,
			Features: ready,
			Status:   "pending",
		}
		batches = append(batches, batch)

		// 移除已处理节点，更新入度
		for _, id := range ready {
			inDegree[id] = -1 // 标记已处理
			processed++

			// 回写 batch 字段到 feature
			batchVal := batchNum
			featureIndex[id].Batch = &batchVal

			// 更新依赖该节点的其他节点入度
			for _, depID := range dependents[id] {
				if inDegree[depID] > 0 {
					inDegree[depID]--
				}
			}
		}

		batchNum++
	}

	plan := &ExecutionPlan{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Batches:     batches,
	}

	return plan, nil
}

// ScheduleRemaining 对未完成的 features 重新调度
// 已完成的 feature 的依赖视为已满足
func (s *Scheduler) ScheduleRemaining(fl *FeatureList) (*ExecutionPlan, error) {
	pending := fl.PendingFeatures()
	if len(pending) == 0 {
		return &ExecutionPlan{
			GeneratedAt: time.Now().Format(time.RFC3339),
			Batches:     nil,
		}, nil
	}

	// 构建仅包含未完成 features 的子图
	completedSet := make(map[string]bool)
	for _, f := range fl.CompletedFeatures() {
		completedSet[f.ID] = true
	}

	// 创建临时 FeatureList，过滤掉已完成的依赖
	subFL := &FeatureList{Features: make([]Feature, len(pending))}
	for i, f := range pending {
		subFL.Features[i] = f
		// 过滤掉已完成的依赖（视为已满足）
		var activeDeps []string
		for _, dep := range f.DependsOn {
			if !completedSet[dep] {
				activeDeps = append(activeDeps, dep)
			}
		}
		subFL.Features[i].DependsOn = activeDeps
		subFL.Features[i].Batch = nil // 重置 batch 分配
	}

	return s.Schedule(subFL)
}
