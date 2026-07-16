package viewdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

// SimulationStore 使用 MySQL 持久化随机指标配置和最近运行状态。
type SimulationStore struct {
	db *gorm.DB
}

var _ nodemetric.SimulationConfigStore = (*SimulationStore)(nil)

// NewSimulationStore 创建随机模拟配置仓储。
func NewSimulationStore(db *gorm.DB) (*SimulationStore, error) {
	if db == nil {
		return nil, errors.New("viewdb: simulation database is required")
	}
	return &SimulationStore{db: db}, nil
}

// List 按有向边和协议稳定排序返回全部模拟配置。
func (s *SimulationStore) List(ctx context.Context) ([]nodemetric.SimulationConfig, error) {
	models := []EdgeMetricSimulationModel{}
	if err := s.db.WithContext(ctx).
		Order("from_node_id, to_node_id, proto").
		Find(&models).Error; err != nil {
		return nil, fmt.Errorf("listing metric simulations: %w", err)
	}

	configs := make([]nodemetric.SimulationConfig, 0, len(models))
	for _, model := range models {
		config, err := simulationConfigFromModel(model)
		if err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}
	return configs, nil
}

// Replace 原子 upsert 请求中包含的配置，未包含的配置保持不变。
func (s *SimulationStore) Replace(ctx context.Context, configs []nodemetric.SimulationConfig) error {
	if len(configs) == 0 {
		return fmt.Errorf("%w: configs are required", nodemetric.ErrInvalidSimulationConfig)
	}
	for _, config := range configs {
		if err := nodemetric.ValidateSimulationConfig(config); err != nil {
			return err
		}
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, config := range configs {
			var edgeCount int64
			if err := tx.Model(&EdgeModel{}).
				Where("from_node_id = ? AND to_node_id = ?", config.FromNodeID, config.ToNodeID).
				Count(&edgeCount).Error; err != nil {
				return fmt.Errorf("checking simulation edge %s: %w", config.EdgeKey.String(), err)
			}
			if edgeCount != 1 {
				return fmt.Errorf("%w: %s", nodemetric.ErrUnknownEdge, config.EdgeKey.String())
			}

			model := simulationConfigToModel(config)
			// 只更新配置字段，保留最近生成时间和指标，避免页面在编辑范围后丢失运行状态。
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "from_node_id"},
					{Name: "to_node_id"},
					{Name: "proto"},
				},
				DoUpdates: clause.AssignmentColumns([]string{
					"enabled",
					"interval_ms",
					"latency_min_ms",
					"latency_max_ms",
					"packet_loss_min_ratio",
					"packet_loss_max_ratio",
					"rate_min_bps",
					"rate_max_bps",
					"updated_at",
				}),
			}).Create(&model).Error; err != nil {
				return fmt.Errorf("upserting simulation %s:%s: %w", config.EdgeKey.String(), config.Protocol, err)
			}
		}
		return nil
	})
}

// UpdateRuntime 保存模拟器最近成功生成的样本，供测试页面展示。
func (s *SimulationStore) UpdateRuntime(ctx context.Context, sample nodemetric.EdgeMetricSample) error {
	metricJSON, err := json.Marshal(sample)
	if err != nil {
		return fmt.Errorf("encoding simulation runtime metric: %w", err)
	}
	generatedAt := sample.ObservedAt.UTC()
	result := s.db.WithContext(ctx).
		Model(&EdgeMetricSimulationModel{}).
		Where(
			"from_node_id = ? AND to_node_id = ? AND proto = ?",
			sample.FromNodeID,
			sample.ToNodeID,
			string(sample.Protocol),
		).
		Updates(map[string]any{
			"last_generated_at": generatedAt,
			"last_metric_json":  metricJSON,
		})
	if result.Error != nil {
		return fmt.Errorf("updating simulation runtime: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: %s:%s", nodemetric.ErrUnknownEdge, sample.EdgeKey.String(), sample.Protocol)
	}
	return nil
}

func simulationConfigToModel(config nodemetric.SimulationConfig) EdgeMetricSimulationModel {
	return EdgeMetricSimulationModel{
		FromNodeID:      config.FromNodeID,
		ToNodeID:        config.ToNodeID,
		Protocol:        string(config.Protocol),
		Enabled:         config.IsEnabled,
		IntervalMS:      uint64(config.Interval / time.Millisecond),
		LatencyMinMS:    config.LatencyMinMS,
		LatencyMaxMS:    config.LatencyMaxMS,
		PacketLossMin:   config.PacketLossMinRatio,
		PacketLossMax:   config.PacketLossMaxRatio,
		RateMinBPS:      config.RateMinBPS,
		RateMaxBPS:      config.RateMaxBPS,
		LastGeneratedAt: config.LastGeneratedAt,
	}
}

func simulationConfigFromModel(model EdgeMetricSimulationModel) (nodemetric.SimulationConfig, error) {
	config := nodemetric.SimulationConfig{
		EdgeKey: nodemetric.EdgeKey{
			FromNodeID: model.FromNodeID,
			ToNodeID:   model.ToNodeID,
		},
		Protocol:           nodemetric.Protocol(model.Protocol),
		IsEnabled:          model.Enabled,
		Interval:           time.Duration(model.IntervalMS) * time.Millisecond,
		LatencyMinMS:       model.LatencyMinMS,
		LatencyMaxMS:       model.LatencyMaxMS,
		PacketLossMinRatio: model.PacketLossMin,
		PacketLossMaxRatio: model.PacketLossMax,
		RateMinBPS:         model.RateMinBPS,
		RateMaxBPS:         model.RateMaxBPS,
		LastGeneratedAt:    model.LastGeneratedAt,
	}
	if len(model.LastMetricJSON) > 0 {
		var metric nodemetric.EdgeMetricSample
		if err := json.Unmarshal(model.LastMetricJSON, &metric); err != nil {
			return nodemetric.SimulationConfig{}, fmt.Errorf("decoding simulation runtime metric: %w", err)
		}
		config.LastMetric = &metric
	}
	return config, nil
}
