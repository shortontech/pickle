package cooked

import (
	"reflect"
	"testing"
)

func TestAppendOnlyQueryBuilderMethodSetExcludesMutations(t *testing.T) {
	typ := reflect.TypeOf(AppendOnlyQuery[testModel]("inventory_movements"))
	for _, method := range []string{"Update", "Delete"} {
		if _, ok := typ.MethodByName(method); ok {
			t.Fatalf("append-only query builder exposes %s", method)
		}
	}
	for _, method := range []string{"First", "All", "Count", "Create"} {
		if _, ok := typ.MethodByName(method); !ok {
			t.Fatalf("append-only query builder is missing %s", method)
		}
	}
}
