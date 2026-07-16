package viewdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"

	"github.com/jursonmo/pathroute/graph"
	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrAlreadyExist = errors.New("already exists")
	ErrInvalidInput = errors.New("invalid input")
)

const (
	nodeStatusUnavailable = 0
	nodeStatusAvailable   = 1
	edgeStatusUnavailable = 0
	edgeStatusAvailable   = 1
)

type NodeDTO struct {
	NodeID string  `json:"nodeId"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Des    string  `json:"des"`
	Type   int     `json:"type"`
	Status int     `json:"status"`
}

type EdgeDTO struct {
	From          string `json:"from"`
	To            string `json:"to"`
	Cost          int    `json:"cost"`
	StaticCost    int    `json:"static_cost"`
	DynamicCost   *int   `json:"dynamic_cost,omitempty"`
	CostRevision  uint64 `json:"cost_revision"`
	CostDegraded  bool   `json:"cost_degraded"`
	DegradeReason string `json:"degrade_reason,omitempty"`
	CostSource    string `json:"cost_source"`
	Des           string `json:"des"`
	Type          int    `json:"type"`
	Status        int    `json:"status"`
}

type GraphDTO struct {
	Nodes                 []NodeDTO `json:"nodes"`
	Edges                 []EdgeDTO `json:"edges"`
	CostRevision          uint64    `json:"cost_revision"`
	DynamicMetricsEnabled bool      `json:"dynamic_metrics_enabled"`
}

type Store struct {
	db           *gorm.DB
	dynamicCosts bool
}

// StoreOption 配置拓扑仓储的构图行为。
type StoreOption func(*Store)

// WithDynamicCosts 控制构图使用动态快照还是静态 graph_edges.cost。
func WithDynamicCosts(enabled bool) StoreOption {
	return func(store *Store) {
		store.dynamicCosts = enabled
	}
}

func NewStore(db *gorm.DB, options ...StoreOption) *Store {
	store := &Store{db: db}
	for _, option := range options {
		option(store)
	}
	return store
}

// EdgeExists 按起点和终点检查一条有向边，供所有指标来源共用同一信任边界。
func (s *Store) EdgeExists(ctx context.Context, key nodemetric.EdgeKey) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctxOrBG(ctx)).Model(&EdgeModel{}).
		Where("from_node_id = ? AND to_node_id = ?", key.FromNodeID, key.ToNodeID).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("checking directed edge %s: %w", key.String(), err)
	}
	return count == 1, nil
}

func ctxOrBG(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func isDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	// mysql duplicate: Error 1062 (23000): Duplicate entry ...
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") || strings.Contains(msg, "Error 1062")
}

func (s *Store) GetGraph(ctx context.Context) (*GraphDTO, error) {
	if s.dynamicCosts {
		return s.getDynamicGraph(ctx)
	}

	nodes := []NodeModel{}
	if err := s.db.WithContext(ctxOrBG(ctx)).Order("node_id asc").Find(&nodes).Error; err != nil {
		return nil, err
	}
	edges := []EdgeModel{}
	if err := s.db.WithContext(ctxOrBG(ctx)).Order("from_node_id asc, to_node_id asc").Find(&edges).Error; err != nil {
		return nil, err
	}
	return buildGraphDTO(nodes, edges, nil, false, 0), nil
}

func (s *Store) getDynamicGraph(ctx context.Context) (*GraphDTO, error) {
	var result *GraphDTO
	err := s.db.WithContext(ctxOrBG(ctx)).Transaction(func(tx *gorm.DB) error {
		var publication EdgeCostPublicationModel
		revision := uint64(0)
		if err := tx.Order("revision DESC").First(&publication).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("loading graph cost revision: %w", err)
			}
		} else {
			revision = publication.Revision
		}

		nodes := []NodeModel{}
		if err := tx.Order("node_id asc").Find(&nodes).Error; err != nil {
			return fmt.Errorf("loading graph nodes: %w", err)
		}
		edges := []EdgeModel{}
		if err := tx.Order("from_node_id asc, to_node_id asc").Find(&edges).Error; err != nil {
			return fmt.Errorf("loading graph edges: %w", err)
		}
		snapshots := []EdgeCostSnapshotModel{}
		if revision > 0 {
			if err := tx.Where("revision <= ?", revision).
				Order("from_node_id asc, to_node_id asc").
				Find(&snapshots).Error; err != nil {
				return fmt.Errorf("loading graph cost snapshots: %w", err)
			}
		}
		result = buildGraphDTO(nodes, edges, snapshots, true, revision)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func buildGraphDTO(
	nodes []NodeModel,
	edges []EdgeModel,
	snapshots []EdgeCostSnapshotModel,
	dynamicCosts bool,
	revision uint64,
) *GraphDTO {
	out := &GraphDTO{
		Nodes:                 make([]NodeDTO, 0, len(nodes)),
		Edges:                 make([]EdgeDTO, 0, len(edges)),
		CostRevision:          revision,
		DynamicMetricsEnabled: dynamicCosts,
	}
	snapshotsByEdge := make(map[string]EdgeCostSnapshotModel, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.Revision <= revision {
			key := snapshot.FromNodeID + "->" + snapshot.ToNodeID
			snapshotsByEdge[key] = snapshot
		}
	}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, NodeDTO{
			NodeID: n.NodeID,
			X:      n.X,
			Y:      n.Y,
			Des:    n.Des,
			Type:   n.Type,
			Status: n.Status,
		})
	}
	for _, e := range edges {
		edge := EdgeDTO{
			From:         e.FromNodeID,
			To:           e.ToNodeID,
			Cost:         e.Cost,
			StaticCost:   e.Cost,
			CostRevision: revision,
			CostSource:   "static",
			Des:          e.Des,
			Type:         e.Type,
			Status:       e.Status,
		}
		if dynamicCosts {
			edge.Cost = 1000
			edge.CostSource = "dynamic_fallback"
			edge.CostDegraded = true
			edge.DegradeReason = "no_dynamic_snapshot"
			key := e.FromNodeID + "->" + e.ToNodeID
			if snapshot, exists := snapshotsByEdge[key]; exists {
				dynamicCost := snapshot.Cost
				edge.Cost = snapshot.Cost
				edge.DynamicCost = &dynamicCost
				edge.CostRevision = snapshot.Revision
				edge.CostSource = "dynamic"
				edge.CostDegraded = snapshot.Degraded
				edge.DegradeReason = snapshot.DegradeReason
			}
		}
		out.Edges = append(out.Edges, edge)
	}
	return out
}

func (s *Store) BuildGraph(ctx context.Context) (*graph.Graph, error) {
	g, _, err := s.BuildGraphWithRevision(ctx)
	return g, err
}

// BuildGraphWithRevision 构造路由图并返回本次一致性事务读取到的 cost revision。
func (s *Store) BuildGraphWithRevision(ctx context.Context) (*graph.Graph, uint64, error) {
	gdto, err := s.GetGraph(ctx)
	if err != nil {
		return nil, 0, err
	}
	g, err := graphFromDTO(gdto)
	if err != nil {
		return nil, 0, err
	}
	return g, gdto.CostRevision, nil
}

func graphFromDTO(gdto *GraphDTO) (*graph.Graph, error) {
	return graph.NewFromStruct(buildAvailableGraphJSON(gdto))
}

func isNodeAvailable(status int) bool {
	return status == nodeStatusAvailable
}

func isEdgeAvailable(status int) bool {
	return status == edgeStatusAvailable
}

func buildAvailableGraphJSON(gdto *GraphDTO) *graph.GraphJSON {
	availableNodes := make(map[string]struct{}, len(gdto.Nodes))
	gj := &graph.GraphJSON{
		Nodes: make([]string, 0, len(gdto.Nodes)),
		Edges: make([]graph.Edge, 0, len(gdto.Edges)),
	}
	for _, n := range gdto.Nodes {
		if !isNodeAvailable(n.Status) {
			continue
		}
		availableNodes[n.NodeID] = struct{}{}
		gj.Nodes = append(gj.Nodes, n.NodeID)
	}
	for _, e := range gdto.Edges {
		if !isEdgeAvailable(e.Status) {
			continue
		}
		if _, ok := availableNodes[e.From]; !ok {
			continue
		}
		if _, ok := availableNodes[e.To]; !ok {
			continue
		}
		gj.Edges = append(gj.Edges, graph.Edge{
			From:   e.From,
			To:     e.To,
			Cost:   e.Cost,
			Des:    e.Des,
			Type:   e.Type,
			Status: e.Status,
		})
	}
	return gj
}

func (s *Store) AddNode(ctx context.Context, n NodeDTO) error {
	if strings.TrimSpace(n.NodeID) == "" {
		return fmt.Errorf("%w: nodeId required", ErrInvalidInput)
	}
	err := s.db.WithContext(ctxOrBG(ctx)).Create(&NodeModel{
		NodeID: n.NodeID,
		X:      n.X,
		Y:      n.Y,
		Des:    n.Des,
		Type:   n.Type,
		Status: n.Status,
	}).Error
	if isDuplicateErr(err) {
		return ErrAlreadyExist
	}
	return err
}

func (s *Store) SavePosition(ctx context.Context, nodeID string, x, y float64) error {
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("%w: nodeId required", ErrInvalidInput)
	}
	tx := s.db.WithContext(ctxOrBG(ctx)).
		Model(&NodeModel{}).
		Where("node_id = ?", nodeID).
		Updates(map[string]interface{}{"x": x, "y": y})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateNode(ctx context.Context, nodeID, des string, typ *int, status *int) error {
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("%w: nodeId required", ErrInvalidInput)
	}
	updates := map[string]interface{}{"des": des}
	if typ != nil {
		updates["type"] = *typ
	}
	if status != nil {
		updates["status"] = *status
	}
	tx := s.db.WithContext(ctxOrBG(ctx)).
		Model(&NodeModel{}).
		Where("node_id = ?", nodeID).
		Updates(updates)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AddEdge(ctx context.Context, e EdgeDTO) error {
	if strings.TrimSpace(e.From) == "" || strings.TrimSpace(e.To) == "" {
		return fmt.Errorf("%w: from/to required", ErrInvalidInput)
	}
	if e.From == e.To {
		return fmt.Errorf("%w: from and to must differ", ErrInvalidInput)
	}
	if e.Cost < 1 || e.Cost > 1000 {
		return fmt.Errorf("%w: cost must be 1-1000", ErrInvalidInput)
	}

	var cnt int64
	if err := s.db.WithContext(ctxOrBG(ctx)).
		Model(&NodeModel{}).
		Where("node_id IN ?", []string{e.From, e.To}).
		Count(&cnt).Error; err != nil {
		return err
	}
	if cnt != 2 {
		return ErrNotFound
	}

	err := s.db.WithContext(ctxOrBG(ctx)).Create(&EdgeModel{
		FromNodeID: e.From,
		ToNodeID:   e.To,
		Cost:       e.Cost,
		Des:        e.Des,
		Type:       e.Type,
		Status:     e.Status,
	}).Error
	if isDuplicateErr(err) {
		return ErrAlreadyExist
	}
	return err
}

// UpdateEdge updates directed edge (from->to). cost==0 means keep existing cost.
func (s *Store) UpdateEdge(ctx context.Context, from, to string, cost int, des string, typ *int, status *int) error {
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
		return fmt.Errorf("%w: from/to required", ErrInvalidInput)
	}
	if cost != 0 && (cost < 1 || cost > 1000) {
		return fmt.Errorf("%w: cost must be 1-1000", ErrInvalidInput)
	}
	updates := map[string]interface{}{"des": des}
	if cost != 0 {
		updates["cost"] = cost
	}
	if typ != nil {
		updates["type"] = *typ
	}
	if status != nil {
		updates["status"] = *status
	}

	tx := s.db.WithContext(ctxOrBG(ctx)).
		Model(&EdgeModel{}).
		Where("from_node_id = ? AND to_node_id = ?", from, to).
		Updates(updates)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SeedFromJSONIfEmpty(ctx context.Context, path string) error {
	var cnt int64
	if err := s.db.WithContext(ctxOrBG(ctx)).Model(&NodeModel{}).Count(&cnt).Error; err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var seed struct {
		Nodes []struct {
			NodeID string  `json:"nodeId"`
			ID     string  `json:"id"` // backward compatibility
			X      float64 `json:"x"`
			Y      float64 `json:"y"`
			Des    string  `json:"des"`
			Type   int     `json:"type"`
			Status int     `json:"status"`
		} `json:"nodes"`
		Edges []struct {
			From   string `json:"from"`
			To     string `json:"to"`
			Cost   int    `json:"cost"`
			Weight int    `json:"weight"` // backward compatibility
			Des    string `json:"des"`
			Type   int    `json:"type"`
			Status int    `json:"status"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(b, &seed); err != nil {
		return err
	}

	return s.db.WithContext(ctxOrBG(ctx)).Transaction(func(tx *gorm.DB) error {
		for _, n := range seed.Nodes {
			nodeID := n.NodeID
			if nodeID == "" {
				nodeID = n.ID
			}
			if strings.TrimSpace(nodeID) == "" {
				continue
			}
			if err := tx.Create(&NodeModel{
				NodeID: nodeID,
				X:      n.X,
				Y:      n.Y,
				Des:    n.Des,
				Type:   n.Type,
				Status: n.Status,
			}).Error; err != nil && !isDuplicateErr(err) {
				return err
			}
		}

		for _, e := range seed.Edges {
			cost := e.Cost
			if cost == 0 {
				cost = e.Weight
			}
			if strings.TrimSpace(e.From) == "" || strings.TrimSpace(e.To) == "" {
				continue
			}
			if cost < 1 || cost > 1000 {
				continue
			}
			if err := tx.Create(&EdgeModel{
				FromNodeID: e.From,
				ToNodeID:   e.To,
				Cost:       cost,
				Des:        e.Des,
				Type:       e.Type,
				Status:     e.Status,
			}).Error; err != nil && !isDuplicateErr(err) {
				return err
			}
		}
		return nil
	})
}
