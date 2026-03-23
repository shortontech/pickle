package generator

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/shortontech/pickle/pkg/schema"
)

// searchColumnInfo holds what the search generator needs for each column.
type searchColumnInfo struct {
	SnakeName   string
	PascalName  string
	GoType      string
	ColType     schema.ColumnType
	IsPublic    bool
	IsOwner     bool // IsPublic || IsOwnerSees
	IsEncrypted bool // AES-SIV deterministic — equality only, no range/like/sort
	IsSealed    bool // AES-GCM non-deterministic — no search at all
}

// generateSearchMethods emits SearchPublic, SearchOwner, SearchAll for a table.
func generateSearchMethods(b *bytes.Buffer, table *schema.Table, queryType, structName string) {
	var cols []searchColumnInfo
	hasVisibility := false
	for _, col := range table.Columns {
		isPublic := col.IsPublic
		isOwner := col.IsPublic || col.IsOwnerSees
		if isPublic || col.IsOwnerSees {
			hasVisibility = true
		}
		cols = append(cols, searchColumnInfo{
			SnakeName:   col.Name,
			PascalName:  snakeToPascal(col.Name),
			GoType:      columnGoType(col),
			ColType:     col.Type,
			IsPublic:    isPublic,
			IsOwner:     isOwner,
			IsEncrypted: col.IsEncrypted,
			IsSealed:    col.IsSealed,
		})
	}

	type searchLevel struct {
		Name       string
		Visibility string
		Filter     func(searchColumnInfo) bool
	}

	levels := []searchLevel{
		{"SearchAll", "visibilityAll", func(_ searchColumnInfo) bool { return true }},
	}

	if hasVisibility {
		levels = append([]searchLevel{
			{"SearchPublic", "visibilityPublic", func(c searchColumnInfo) bool { return c.IsPublic }},
			{"SearchOwner", "visibilityOwner", func(c searchColumnInfo) bool { return c.IsOwner }},
		}, levels...)
	}

	for _, level := range levels {
		writeSearchMethod(b, queryType, structName, level.Name, level.Visibility, cols, level.Filter)
	}
}

func writeSearchMethod(b *bytes.Buffer, queryType, structName, methodName, visibility string, cols []searchColumnInfo, include func(searchColumnInfo) bool) {
	b.WriteString(fmt.Sprintf("// %s parses JSON API query params and returns filtered, sorted, paginated %s results.\n", methodName, structName))
	b.WriteString(fmt.Sprintf("func (q *%s) %s(r *http.Request) ([]%s, *Pagination, error) {\n", queryType, methodName, structName))
	b.WriteString(fmt.Sprintf("\tq.setVisibility(%s)\n\n", visibility))

	// Filter switch
	b.WriteString("\tfor key, val := range parseQueryFilters(r) {\n")
	b.WriteString("\t\tswitch key {\n")
	for _, col := range cols {
		if !include(col) || !isSearchable(col.ColType) {
			continue
		}
		// Sealed columns cannot be searched at all (non-deterministic encryption)
		if col.IsSealed {
			continue
		}
		b.WriteString(fmt.Sprintf("\t\tcase %q:\n", col.SnakeName))
		writeFilterCase(b, col, "val")
	}
	b.WriteString("\t\tdefault:\n")
	b.WriteString("\t\t\treturn nil, nil, fmt.Errorf(\"unknown filter: %s\", key)\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n\n")

	// Operator filter switch
	b.WriteString("\tfor _, fop := range parseQueryFilterOps(r) {\n")
	b.WriteString("\t\tswitch fop.Column {\n")
	for _, col := range cols {
		if !include(col) || !isSearchable(col.ColType) {
			continue
		}
		// Encrypted/sealed columns: no range or like operators
		if col.IsEncrypted || col.IsSealed {
			continue
		}
		ops := opsForType(col.ColType)
		if len(ops) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("\t\tcase %q:\n", col.SnakeName))
		b.WriteString("\t\t\tswitch fop.Operator {\n")
		for _, op := range ops {
			b.WriteString(fmt.Sprintf("\t\t\tcase %q:\n", op.name))
			writeOpFilterCase(b, col, op)
		}
		b.WriteString("\t\t\tdefault:\n")
		b.WriteString("\t\t\t\treturn nil, nil, fmt.Errorf(\"unknown operator %s for filter %s\", fop.Operator, fop.Column)\n")
		b.WriteString("\t\t\t}\n")
	}
	b.WriteString("\t\tdefault:\n")
	b.WriteString("\t\t\treturn nil, nil, fmt.Errorf(\"unknown filter: %s\", fop.Column)\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n\n")

	// Sort switch
	b.WriteString("\tif sortCol, sortDir := parseQuerySort(r); sortCol != \"\" {\n")
	b.WriteString("\t\tswitch sortCol {\n")
	for _, col := range cols {
		if !include(col) || !isSearchable(col.ColType) {
			continue
		}
		// Encrypted/sealed columns: ordering ciphertext is meaningless
		if col.IsEncrypted || col.IsSealed {
			continue
		}
		b.WriteString(fmt.Sprintf("\t\tcase %q:\n", col.SnakeName))
		b.WriteString(fmt.Sprintf("\t\t\tq.OrderBy(%q, sortDir)\n", col.SnakeName))
	}
	b.WriteString("\t\tdefault:\n")
	b.WriteString("\t\t\treturn nil, nil, fmt.Errorf(\"unknown sort field: %s\", sortCol)\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n\n")

	// Pagination
	b.WriteString("\tpage, pageSize := parseQueryPage(r)\n")
	b.WriteString("\ttotal, err := q.Count()\n")
	b.WriteString("\tif err != nil {\n")
	b.WriteString("\t\treturn nil, nil, err\n")
	b.WriteString("\t}\n")
	b.WriteString("\tq.Limit(pageSize)\n")
	b.WriteString("\tq.Offset((page - 1) * pageSize)\n\n")
	b.WriteString("\tresults, err := q.All()\n")
	b.WriteString("\tif err != nil {\n")
	b.WriteString("\t\treturn nil, nil, err\n")
	b.WriteString("\t}\n\n")
	b.WriteString("\tpages := int(total) / pageSize\n")
	b.WriteString("\tif int(total)%pageSize != 0 {\n")
	b.WriteString("\t\tpages++\n")
	b.WriteString("\t}\n\n")
	b.WriteString("\treturn results, &Pagination{\n")
	b.WriteString("\t\tTotal:    total,\n")
	b.WriteString("\t\tPage:     page,\n")
	b.WriteString("\t\tPageSize: pageSize,\n")
	b.WriteString("\t\tPages:    pages,\n")
	b.WriteString("\t}, nil\n")
	b.WriteString("}\n\n")
}

// isSearchable returns true if a column type can be filtered/sorted in search.
func isSearchable(colType schema.ColumnType) bool {
	switch colType {
	case schema.JSONB, schema.Binary:
		return false
	default:
		return true
	}
}

type filterOp struct {
	name         string
	methodSuffix string
}

func opsForType(colType schema.ColumnType) []filterOp {
	switch colType {
	case schema.String, schema.Text:
		return []filterOp{
			{"like", "Like"},
			{"not_like", "NotLike"},
		}
	case schema.Integer, schema.BigInteger, schema.Decimal:
		return []filterOp{
			{"gt", "GT"},
			{"gte", "GTE"},
			{"lt", "LT"},
			{"lte", "LTE"},
		}
	case schema.Timestamp, schema.Date:
		return []filterOp{
			{"gt", "After"},
			{"gte", "After"},
			{"lt", "Before"},
			{"lte", "Before"},
		}
	default:
		return nil
	}
}

// whereArg returns "parsed" or "&parsed" depending on whether the column is nullable.
func whereArg(col searchColumnInfo) string {
	if strings.HasPrefix(col.GoType, "*") {
		return "&parsed"
	}
	return "parsed"
}

func writeFilterCase(b *bytes.Buffer, col searchColumnInfo, valVar string) {
	nullable := strings.HasPrefix(col.GoType, "*")
	switch col.ColType {
	case schema.UUID:
		b.WriteString(fmt.Sprintf("\t\t\tparsed, err := uuid.Parse(%s)\n", valVar))
		b.WriteString("\t\t\tif err != nil {\n")
		b.WriteString("\t\t\t\treturn nil, nil, fmt.Errorf(\"invalid uuid for filter %s: %w\", key, err)\n")
		b.WriteString("\t\t\t}\n")
		b.WriteString(fmt.Sprintf("\t\t\tq.Where%s(%s)\n", col.PascalName, whereArg(col)))
	case schema.Integer:
		b.WriteString(fmt.Sprintf("\t\t\tparsed, err := strconv.Atoi(%s)\n", valVar))
		b.WriteString("\t\t\tif err != nil {\n")
		b.WriteString("\t\t\t\treturn nil, nil, fmt.Errorf(\"invalid integer for filter %s: %w\", key, err)\n")
		b.WriteString("\t\t\t}\n")
		b.WriteString(fmt.Sprintf("\t\t\tq.Where%s(%s)\n", col.PascalName, whereArg(col)))
	case schema.BigInteger:
		b.WriteString(fmt.Sprintf("\t\t\tparsed, err := strconv.ParseInt(%s, 10, 64)\n", valVar))
		b.WriteString("\t\t\tif err != nil {\n")
		b.WriteString("\t\t\t\treturn nil, nil, fmt.Errorf(\"invalid integer for filter %s: %w\", key, err)\n")
		b.WriteString("\t\t\t}\n")
		b.WriteString(fmt.Sprintf("\t\t\tq.Where%s(%s)\n", col.PascalName, whereArg(col)))
	case schema.Decimal:
		b.WriteString(fmt.Sprintf("\t\t\tparsed, err := decimal.NewFromString(%s)\n", valVar))
		b.WriteString("\t\t\tif err != nil {\n")
		b.WriteString("\t\t\t\treturn nil, nil, fmt.Errorf(\"invalid decimal for filter %s: %w\", key, err)\n")
		b.WriteString("\t\t\t}\n")
		b.WriteString(fmt.Sprintf("\t\t\tq.Where%s(%s)\n", col.PascalName, whereArg(col)))
	case schema.Timestamp, schema.Date:
		b.WriteString(fmt.Sprintf("\t\t\tparsed, err := time.Parse(time.RFC3339, %s)\n", valVar))
		b.WriteString("\t\t\tif err != nil {\n")
		b.WriteString("\t\t\t\treturn nil, nil, fmt.Errorf(\"invalid timestamp for filter %s: %w\", key, err)\n")
		b.WriteString("\t\t\t}\n")
		b.WriteString(fmt.Sprintf("\t\t\tq.Where%s(%s)\n", col.PascalName, whereArg(col)))
	case schema.Boolean:
		b.WriteString(fmt.Sprintf("\t\t\tparsed, err := strconv.ParseBool(%s)\n", valVar))
		b.WriteString("\t\t\tif err != nil {\n")
		b.WriteString("\t\t\t\treturn nil, nil, fmt.Errorf(\"invalid boolean for filter %s: %w\", key, err)\n")
		b.WriteString("\t\t\t}\n")
		b.WriteString(fmt.Sprintf("\t\t\tq.Where%s(%s)\n", col.PascalName, whereArg(col)))
	default:
		// string, text — no parsing needed
		if nullable {
			b.WriteString(fmt.Sprintf("\t\t\tp := %s\n", valVar))
			b.WriteString(fmt.Sprintf("\t\t\tq.Where%s(&p)\n", col.PascalName))
		} else {
			b.WriteString(fmt.Sprintf("\t\t\tq.Where%s(%s)\n", col.PascalName, valVar))
		}
	}
}

func writeOpFilterCase(b *bytes.Buffer, col searchColumnInfo, op filterOp) {
	switch col.ColType {
	case schema.String, schema.Text:
		b.WriteString(fmt.Sprintf("\t\t\t\tq.Where%s%s(fop.Value)\n", col.PascalName, op.methodSuffix))
	case schema.Integer:
		b.WriteString("\t\t\t\tparsed, err := strconv.Atoi(fop.Value)\n")
		b.WriteString("\t\t\t\tif err != nil {\n")
		b.WriteString("\t\t\t\t\treturn nil, nil, fmt.Errorf(\"invalid integer for filter %s[%s]: %w\", fop.Column, fop.Operator, err)\n")
		b.WriteString("\t\t\t\t}\n")
		b.WriteString(fmt.Sprintf("\t\t\t\tq.Where%s%s(parsed)\n", col.PascalName, op.methodSuffix))
	case schema.BigInteger:
		b.WriteString("\t\t\t\tparsed, err := strconv.ParseInt(fop.Value, 10, 64)\n")
		b.WriteString("\t\t\t\tif err != nil {\n")
		b.WriteString("\t\t\t\t\treturn nil, nil, fmt.Errorf(\"invalid integer for filter %s[%s]: %w\", fop.Column, fop.Operator, err)\n")
		b.WriteString("\t\t\t\t}\n")
		b.WriteString(fmt.Sprintf("\t\t\t\tq.Where%s%s(parsed)\n", col.PascalName, op.methodSuffix))
	case schema.Decimal:
		b.WriteString("\t\t\t\tparsed, err := decimal.NewFromString(fop.Value)\n")
		b.WriteString("\t\t\t\tif err != nil {\n")
		b.WriteString("\t\t\t\t\treturn nil, nil, fmt.Errorf(\"invalid decimal for filter %s[%s]: %w\", fop.Column, fop.Operator, err)\n")
		b.WriteString("\t\t\t\t}\n")
		b.WriteString(fmt.Sprintf("\t\t\t\tq.Where%s%s(parsed)\n", col.PascalName, op.methodSuffix))
	case schema.Timestamp, schema.Date:
		b.WriteString("\t\t\t\tparsed, err := time.Parse(time.RFC3339, fop.Value)\n")
		b.WriteString("\t\t\t\tif err != nil {\n")
		b.WriteString("\t\t\t\t\treturn nil, nil, fmt.Errorf(\"invalid timestamp for filter %s[%s]: %w\", fop.Column, fop.Operator, err)\n")
		b.WriteString("\t\t\t\t}\n")
		b.WriteString(fmt.Sprintf("\t\t\t\tq.Where%s%s(parsed)\n", col.PascalName, op.methodSuffix))
	}
}
