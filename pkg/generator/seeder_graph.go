package generator

import (
	"fmt"
	"strings"

	"github.com/shortontech/pickle/pkg/schema"
)

// ResolvedSeedRelationship binds one child graph node to an ordered schema FK.
type ResolvedSeedRelationship struct {
	ChildNodeID       int
	ParentNodeID      int
	Columns           []string
	ReferencedTable   string
	ReferencedColumns []string
}

// ValidateSeedGraph resolves graph edges against authoritative schema metadata.
func ValidateSeedGraph(graph *schema.SeedGraph, tables []*schema.Table) ([]ResolvedSeedRelationship, error) {
	tableByName := map[string]*schema.Table{}
	for _, table := range tables {
		tableByName[table.Name] = table
	}
	nodeByID := map[int]schema.SeedNode{}
	for _, node := range graph.Nodes {
		if tableByName[node.Seeder.Table] == nil {
			return nil, fmt.Errorf("seeder %s targets unknown table %q", node.Seeder.Name, node.Seeder.Table)
		}
		if node.Count.Min < 0 || node.Count.Min > node.Count.Max {
			return nil, fmt.Errorf("seeder %s has invalid count range", node.Seeder.Name)
		}
		for column := range node.Values {
			if !tableHasColumn(tableByName[node.Seeder.Table], column) {
				return nil, fmt.Errorf("seeder %s overrides unknown column %s.%s", node.Seeder.Name, node.Seeder.Table, column)
			}
		}
		for _, column := range append(append([]string(nil), node.UniqueColumns...), node.UpdateColumns...) {
			if !tableHasColumn(tableByName[node.Seeder.Table], column) {
				return nil, fmt.Errorf("seeder %s repeat policy references unknown column %s.%s", node.Seeder.Name, node.Seeder.Table, column)
			}
		}
		for _, column := range node.UpdateColumns {
			if containsSeedColumn(node.UniqueColumns, column) {
				return nil, fmt.Errorf("seeder %s cannot update identity column %s", node.Seeder.Name, column)
			}
		}
		nodeByID[node.ID] = node
	}

	var resolved []ResolvedSeedRelationship
	for _, child := range graph.Nodes {
		if child.ParentNodeID == 0 {
			continue
		}
		parent, ok := nodeByID[child.ParentNodeID]
		if !ok {
			return nil, fmt.Errorf("seeder %s references missing parent node %d", child.Seeder.Name, child.ParentNodeID)
		}
		matches := relationshipCandidates(tableByName[child.Seeder.Table], parent.Seeder.Table, child.Through)
		if len(matches) == 0 {
			return nil, fmt.Errorf("seeder %s has no relationship from %s to %s", child.Seeder.Name, child.Seeder.Table, parent.Seeder.Table)
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("seeder %s has ambiguous relationship from %s to %s; specify Through", child.Seeder.Name, child.Seeder.Table, parent.Seeder.Table)
		}
		match := matches[0]
		for i := range match.Columns {
			if !tableHasColumn(tableByName[child.Seeder.Table], match.Columns[i]) || !tableHasColumn(tableByName[parent.Seeder.Table], match.ReferencedColumns[i]) {
				return nil, fmt.Errorf("seeder %s relationship contains unknown composite column", child.Seeder.Name)
			}
		}
		resolved = append(resolved, ResolvedSeedRelationship{ChildNodeID: child.ID, ParentNodeID: parent.ID, Columns: append([]string(nil), match.Columns...), ReferencedTable: match.ReferencedTable, ReferencedColumns: append([]string(nil), match.ReferencedColumns...)})
	}
	if err := validateSeedNodeCycles(graph.Nodes); err != nil {
		return nil, err
	}
	return resolved, nil
}

func validateSeedNodeCycles(nodes []schema.SeedNode) error {
	parents := map[int]int{}
	for _, node := range nodes {
		parents[node.ID] = node.ParentNodeID
	}
	for _, node := range nodes {
		seen := map[int]bool{}
		for id := node.ID; id != 0; id = parents[id] {
			if seen[id] {
				return fmt.Errorf("seed graph contains a relationship cycle at node %d", id)
			}
			seen[id] = true
		}
	}
	return nil
}

func tableHasColumn(table *schema.Table, name string) bool {
	for _, column := range table.Columns {
		if column.Name == name {
			return true
		}
	}
	return false
}

func relationshipCandidates(child *schema.Table, parentTable, through string) []*schema.ForeignKey {
	var candidates []*schema.ForeignKey
	for _, fk := range child.ForeignKeys {
		if fk.ReferencedTable != parentTable {
			continue
		}
		if through != "" && !containsSeedColumn(fk.Columns, through) && strings.Join(fk.Columns, ",") != through {
			continue
		}
		candidates = append(candidates, fk)
	}
	for _, column := range child.Columns {
		if column.ForeignKeyTable != parentTable {
			continue
		}
		if through != "" && through != column.Name {
			continue
		}
		candidates = append(candidates, &schema.ForeignKey{Columns: []string{column.Name}, ReferencedTable: parentTable, ReferencedColumns: []string{column.ForeignKeyColumn}})
	}
	return candidates
}

func containsSeedColumn(columns []string, target string) bool {
	for _, column := range columns {
		if column == target {
			return true
		}
	}
	return false
}
