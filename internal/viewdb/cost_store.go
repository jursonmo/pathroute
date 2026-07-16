package viewdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

// CostStore 使用 MySQL 原子发布和读取当前动态 cost 快照。
type CostStore struct {
	db *gorm.DB
}

var _ nodemetric.CostSnapshotStore = (*CostStore)(nil)

// NewCostStore 创建动态 cost 快照仓储。
func NewCostStore(db *gorm.DB) (*CostStore, error) {
	if db == nil {
		return nil, errors.New("viewdb: cost database is required")
	}
	return &CostStore{db: db}, nil
}

// Publish 在一个 MySQL 事务中创建 revision 并更新协议、边两层当前快照。
func (s *CostStore) Publish(
	ctx context.Context,
	publication nodemetric.CostPublication,
	protocols []nodemetric.ProtocolCostSnapshot,
	edges []nodemetric.EdgeCostSnapshot,
) (nodemetric.CostPublication, error) {
	if len(edges) == 0 {
		return nodemetric.CostPublication{}, errors.New("viewdb: cost publication requires edges")
	}
	if publication.PublishedAt.IsZero() || publication.FormulaVersion == "" {
		return nodemetric.CostPublication{}, errors.New("viewdb: invalid cost publication metadata")
	}

	publicationModel := EdgeCostPublicationModel{
		TriggerReason:  publication.TriggerReason,
		ChangedEdges:   len(edges),
		FormulaVersion: publication.FormulaVersion,
		PublishedAt:    publication.PublishedAt.UTC(),
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&publicationModel).Error; err != nil {
			return fmt.Errorf("creating cost publication: %w", err)
		}
		revision := publicationModel.Revision

		protocolModels := make([]EdgeProtocolCostSnapshotModel, 0, len(protocols))
		for _, protocol := range protocols {
			protocolModels = append(protocolModels, protocolCostToModel(protocol, revision))
		}
		if len(protocolModels) > 0 {
			if err := tx.Clauses(protocolCostUpsertClause()).Create(&protocolModels).Error; err != nil {
				return fmt.Errorf("publishing protocol cost snapshots: %w", err)
			}
		}

		edgeModels := make([]EdgeCostSnapshotModel, 0, len(edges))
		for _, edge := range edges {
			edgeModels = append(edgeModels, edgeCostToModel(edge, publicationModel))
		}
		if err := tx.Clauses(edgeCostUpsertClause()).Create(&edgeModels).Error; err != nil {
			return fmt.Errorf("publishing edge cost snapshots: %w", err)
		}
		return nil
	})
	if err != nil {
		return nodemetric.CostPublication{}, err
	}
	publication.Revision = publicationModel.Revision
	publication.ChangedEdges = len(edges)
	return publication, nil
}

// RefreshMetrics 原子刷新未触发路由权重发布的协议聚合状态。
// 调用方传入的边 cost 和 revision 已固定为当前发布值，本方法不得创建 publication。
func (s *CostStore) RefreshMetrics(
	ctx context.Context,
	protocols []nodemetric.ProtocolCostSnapshot,
	edges []nodemetric.EdgeCostSnapshot,
) error {
	if len(edges) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		protocolModels := make([]EdgeProtocolCostSnapshotModel, 0, len(protocols))
		for _, protocol := range protocols {
			protocolModels = append(protocolModels, protocolCostToModel(protocol, protocol.Revision))
		}
		if len(protocolModels) > 0 {
			if err := tx.Clauses(protocolCostUpsertClause()).Create(&protocolModels).Error; err != nil {
				return fmt.Errorf("refreshing protocol metric snapshots: %w", err)
			}
		}

		edgeModels := make([]EdgeCostSnapshotModel, 0, len(edges))
		for _, edge := range edges {
			edgeModels = append(edgeModels, edgeCostToModel(edge, EdgeCostPublicationModel{
				Revision:    edge.Revision,
				PublishedAt: edge.PublishedAt,
			}))
		}
		if err := tx.Clauses(edgeCostUpsertClause()).Create(&edgeModels).Error; err != nil {
			return fmt.Errorf("refreshing edge metric snapshots: %w", err)
		}
		return nil
	})
}

// ListEdgeCosts 返回当前边快照，并将对应协议快照按有向边组装到结果中。
func (s *CostStore) ListEdgeCosts(ctx context.Context) ([]nodemetric.EdgeCostSnapshot, error) {
	return listEdgeCosts(s.db.WithContext(ctx))
}

// CurrentEdgeCosts 在同一个可重复读事务中返回 publication 和两层 cost 快照。
// 这样 HTTP 响应不会把发布前后的 revision、边快照和协议快照拼接在一起。
func (s *CostStore) CurrentEdgeCosts(
	ctx context.Context,
) (nodemetric.CostPublication, []nodemetric.EdgeCostSnapshot, error) {
	var publication nodemetric.CostPublication
	var edges []nodemetric.EdgeCostSnapshot
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model EdgeCostPublicationModel
		if err := tx.Order("revision DESC").First(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nodemetric.ErrNoCostSnapshot
			}
			return fmt.Errorf("loading latest cost publication: %w", err)
		}
		publication = costPublicationFromModel(model)

		var err error
		edges, err = listEdgeCosts(tx)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nodemetric.CostPublication{}, nil, err
	}
	return publication, edges, nil
}

func listEdgeCosts(db *gorm.DB) ([]nodemetric.EdgeCostSnapshot, error) {
	edgeModels := []EdgeCostSnapshotModel{}
	if err := db.
		Order("from_node_id, to_node_id").
		Find(&edgeModels).Error; err != nil {
		return nil, fmt.Errorf("listing edge cost snapshots: %w", err)
	}
	protocolModels := []EdgeProtocolCostSnapshotModel{}
	if err := db.
		Order("from_node_id, to_node_id, proto").
		Find(&protocolModels).Error; err != nil {
		return nil, fmt.Errorf("listing protocol cost snapshots: %w", err)
	}

	protocolsByEdge := make(map[string][]nodemetric.ProtocolCostSnapshot, len(edgeModels))
	for _, model := range protocolModels {
		key := nodemetric.EdgeKey{FromNodeID: model.FromNodeID, ToNodeID: model.ToNodeID}
		protocolsByEdge[key.String()] = append(protocolsByEdge[key.String()], protocolCostFromModel(model))
	}
	edges := make([]nodemetric.EdgeCostSnapshot, 0, len(edgeModels))
	for _, model := range edgeModels {
		key := nodemetric.EdgeKey{FromNodeID: model.FromNodeID, ToNodeID: model.ToNodeID}
		protocolCosts := protocolsByEdge[key.String()]
		if protocolCosts == nil {
			protocolCosts = []nodemetric.ProtocolCostSnapshot{}
		}
		edges = append(edges, edgeCostFromModel(model, protocolCosts))
	}
	return edges, nil
}

// LatestPublication 返回最近一次成功提交的全局 cost revision。
func (s *CostStore) LatestPublication(ctx context.Context) (nodemetric.CostPublication, error) {
	var model EdgeCostPublicationModel
	if err := s.db.WithContext(ctx).Order("revision DESC").First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nodemetric.CostPublication{}, nodemetric.ErrNoCostSnapshot
		}
		return nodemetric.CostPublication{}, fmt.Errorf("loading latest cost publication: %w", err)
	}
	return costPublicationFromModel(model), nil
}

func costPublicationFromModel(model EdgeCostPublicationModel) nodemetric.CostPublication {
	return nodemetric.CostPublication{
		Revision:       model.Revision,
		TriggerReason:  model.TriggerReason,
		ChangedEdges:   model.ChangedEdges,
		FormulaVersion: model.FormulaVersion,
		PublishedAt:    model.PublishedAt,
	}
}

func protocolCostToModel(
	protocol nodemetric.ProtocolCostSnapshot,
	revision uint64,
) EdgeProtocolCostSnapshotModel {
	return EdgeProtocolCostSnapshotModel{
		FromNodeID:        protocol.FromNodeID,
		ToNodeID:          protocol.ToNodeID,
		Protocol:          string(protocol.Protocol),
		Cost:              protocol.Cost,
		Expired:           protocol.IsExpired,
		AverageLatencyMS:  protocol.AverageLatencyMS,
		AveragePacketLoss: protocol.AveragePacketLossRate,
		AverageRateBPS:    protocol.AverageRateBPS,
		SampleCount:       protocol.SampleCount,
		WindowStart:       protocol.WindowStart.UTC(),
		WindowEnd:         protocol.WindowEnd.UTC(),
		LatestObservedAt:  protocol.LatestObservedAt.UTC(),
		CalculatedAt:      protocol.CalculatedAt.UTC(),
		FormulaVersion:    protocol.FormulaVersion,
		Revision:          revision,
	}
}

func edgeCostToModel(
	edge nodemetric.EdgeCostSnapshot,
	publication EdgeCostPublicationModel,
) EdgeCostSnapshotModel {
	return EdgeCostSnapshotModel{
		FromNodeID:     edge.FromNodeID,
		ToNodeID:       edge.ToNodeID,
		Cost:           edge.Cost,
		Degraded:       edge.IsDegraded,
		DegradeReason:  edge.DegradeReason,
		WindowStart:    edge.WindowStart.UTC(),
		WindowEnd:      edge.WindowEnd.UTC(),
		LatestMetricAt: edge.LatestMetricAt.UTC(),
		CalculatedAt:   edge.CalculatedAt.UTC(),
		PublishedAt:    publication.PublishedAt.UTC(),
		FormulaVersion: edge.FormulaVersion,
		Revision:       publication.Revision,
	}
}

func protocolCostFromModel(model EdgeProtocolCostSnapshotModel) nodemetric.ProtocolCostSnapshot {
	return nodemetric.ProtocolCostSnapshot{
		AggregatedMetric: nodemetric.AggregatedMetric{
			EdgeKey: nodemetric.EdgeKey{
				FromNodeID: model.FromNodeID,
				ToNodeID:   model.ToNodeID,
			},
			Protocol:              nodemetric.Protocol(model.Protocol),
			AverageLatencyMS:      model.AverageLatencyMS,
			AveragePacketLossRate: model.AveragePacketLoss,
			AverageRateBPS:        model.AverageRateBPS,
			SampleCount:           model.SampleCount,
			WindowStart:           model.WindowStart,
			WindowEnd:             model.WindowEnd,
			LatestObservedAt:      model.LatestObservedAt,
		},
		Cost:           model.Cost,
		IsExpired:      model.Expired,
		FormulaVersion: model.FormulaVersion,
		CalculatedAt:   model.CalculatedAt,
		Revision:       model.Revision,
	}
}

func edgeCostFromModel(
	model EdgeCostSnapshotModel,
	protocols []nodemetric.ProtocolCostSnapshot,
) nodemetric.EdgeCostSnapshot {
	return nodemetric.EdgeCostSnapshot{
		EdgeKey: nodemetric.EdgeKey{
			FromNodeID: model.FromNodeID,
			ToNodeID:   model.ToNodeID,
		},
		Cost:           model.Cost,
		IsDegraded:     model.Degraded,
		DegradeReason:  model.DegradeReason,
		ProtocolCosts:  protocols,
		WindowStart:    model.WindowStart,
		WindowEnd:      model.WindowEnd,
		LatestMetricAt: model.LatestMetricAt,
		FormulaVersion: model.FormulaVersion,
		CalculatedAt:   model.CalculatedAt,
		PublishedAt:    model.PublishedAt,
		Revision:       model.Revision,
	}
}

func protocolCostUpsertClause() clause.OnConflict {
	return clause.OnConflict{
		Columns: []clause.Column{
			{Name: "from_node_id"},
			{Name: "to_node_id"},
			{Name: "proto"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"cost",
			"expired",
			"avg_latency_ms",
			"avg_packet_loss_ratio",
			"avg_rate_bps",
			"sample_count",
			"window_start",
			"window_end",
			"latest_observed_at",
			"calculated_at",
			"formula_version",
			"revision",
			"updated_at",
		}),
	}
}

func edgeCostUpsertClause() clause.OnConflict {
	return clause.OnConflict{
		Columns: []clause.Column{
			{Name: "from_node_id"},
			{Name: "to_node_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"cost",
			"degraded",
			"degrade_reason",
			"window_start",
			"window_end",
			"latest_metric_at",
			"calculated_at",
			"published_at",
			"formula_version",
			"revision",
			"updated_at",
		}),
	}
}
