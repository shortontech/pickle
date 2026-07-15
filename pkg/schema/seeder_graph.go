package schema

import "fmt"

// ScenarioSeeder is embedded by root seed graph declarations.
type ScenarioSeeder struct{}

// SeedRecord is a stable handle to a row produced within one seed graph.
type SeedRecord struct {
	NodeID int
	Table  string
}

// SeedRecords is a collection produced by CreateN.
type SeedRecords struct {
	NodeID int
	Table  string
	Count  int
}

// SeedCount is a fixed or deterministic range used by CreateN.
type SeedCount struct{ Min, Max int }

// SeedNode is one declarative row-production node.
type SeedNode struct {
	ID           int
	Seeder       SeederRef
	Count        SeedCount
	ParentNodeID int
	Through      string
	Values       map[string]any
}

// SeedGraph records scenario intent for validation and generated execution.
type SeedGraph struct{ Nodes []SeedNode }

// FixedCount declares an exact row count.
func FixedCount(count int) SeedCount {
	if count < 0 {
		panic("pickle: seed count must not be negative")
	}
	return SeedCount{Min: count, Max: count}
}

// Between declares an inclusive deterministic row-count range.
func (g *SeedGraph) Between(min, max int) SeedCount {
	if min < 0 || min > max {
		panic("pickle: invalid seed count range")
	}
	return SeedCount{Min: min, Max: max}
}

// Create declares one row.
func (g *SeedGraph) Create(seeder SeederRef) *SeedNodeBuilder {
	return g.CreateN(seeder, FixedCount(1))
}

// CreateN declares a fixed or ranged collection of rows.
func (g *SeedGraph) CreateN(seeder SeederRef, count any) *SeedNodeBuilder {
	if seeder.Name == "" || seeder.Table == "" {
		panic("pickle: row seeder requires a name and table")
	}
	var resolved SeedCount
	switch value := count.(type) {
	case int:
		resolved = FixedCount(value)
	case SeedCount:
		resolved = value
	default:
		panic("pickle: CreateN count must be int or SeedCount")
	}
	node := SeedNode{ID: len(g.Nodes) + 1, Seeder: seeder, Count: resolved, Values: map[string]any{}}
	g.Nodes = append(g.Nodes, node)
	return &SeedNodeBuilder{graph: g, index: len(g.Nodes) - 1}
}

// ForEach declares nested graph shape using a representative handle to every
// row produced by the collection. Generated execution expands the edge.
func (g *SeedGraph) ForEach(records SeedRecords, fn func(SeedRecord)) {
	if records.NodeID < 1 || fn == nil {
		panic("pickle: ForEach requires a collection and callback")
	}
	fn(SeedRecord{NodeID: records.NodeID, Table: records.Table})
}

// SeedNodeBuilder adds relationship and override metadata to a graph node.
type SeedNodeBuilder struct {
	graph *SeedGraph
	index int
}

func (b *SeedNodeBuilder) node() *SeedNode { return &b.graph.Nodes[b.index] }

// For attaches generated rows to a parent. Through is required only when the
// schema has more than one possible relationship.
func (b *SeedNodeBuilder) For(parent SeedRecord, through ...string) *SeedNodeBuilder {
	if parent.NodeID < 1 {
		panic("pickle: seed parent is invalid")
	}
	b.node().ParentNodeID = parent.NodeID
	if len(through) > 1 {
		panic("pickle: For accepts at most one relationship selector")
	}
	if len(through) == 1 {
		b.node().Through = through[0]
	}
	return b
}

// With overrides one schema column for this node.
func (b *SeedNodeBuilder) With(column string, value any) *SeedNodeBuilder {
	if column == "" {
		panic("pickle: seed override column must not be empty")
	}
	if _, exists := b.node().Values[column]; exists {
		panic(fmt.Sprintf("pickle: duplicate seed override %q", column))
	}
	b.node().Values[column] = value
	return b
}

func (b *SeedNodeBuilder) One() SeedRecord {
	if b.node().Count.Min != 1 || b.node().Count.Max != 1 {
		panic("pickle: One requires an exact count of one")
	}
	return SeedRecord{NodeID: b.node().ID, Table: b.node().Seeder.Table}
}

func (b *SeedNodeBuilder) Many() SeedRecords {
	return SeedRecords{NodeID: b.node().ID, Table: b.node().Seeder.Table, Count: b.node().Count.Max}
}

// NewRowSeederRef declares a row seeder token targeting a table.
func NewRowSeederRef(name, table string) SeederRef {
	if name == "" || table == "" {
		panic("pickle: row seeder name and table must not be empty")
	}
	return SeederRef{Name: name, Table: table}
}
