package viewdb

import "time"

// NodeModel represents one graph node in DB.
type NodeModel struct {
	ID        uint      `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	NodeID string  `gorm:"column:node_id;size:128;not null;uniqueIndex"`
	X      float64 `gorm:"column:x;not null;default:0"`
	Y      float64 `gorm:"column:y;not null;default:0"`
	Des    string  `gorm:"column:des;size:512;not null;default:''"`
	Type   int     `gorm:"column:type;not null;default:0"`
	Status int     `gorm:"column:status;not null;default:0"`
}

func (NodeModel) TableName() string { return "graph_nodes" }

// EdgeModel represents one directed graph edge in DB.
type EdgeModel struct {
	ID        uint      `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	FromNodeID string `gorm:"column:from_node_id;size:128;not null;index:idx_from_to,unique"`
	ToNodeID   string `gorm:"column:to_node_id;size:128;not null;index:idx_from_to,unique"`

	Cost   int    `gorm:"column:cost;not null"`
	Des    string `gorm:"column:des;size:512;not null;default:''"`
	Type   int    `gorm:"column:type;not null;default:0"`
	Status int    `gorm:"column:status;not null;default:0"`
}

func (EdgeModel) TableName() string { return "graph_edges" }

