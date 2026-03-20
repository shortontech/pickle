package generator

import (
	"strings"
)

// RequestValidationMap maps GraphQL input struct field names to their validation tags
// from the matching request struct. Key format: "CreateUserInput.Name" → "required,min=1,max=255".
type RequestValidationMap map[string]string

// BuildRequestValidationMap builds a map from request defs to validation tags
// for GraphQL input types. Matches CreateXxxRequest → CreateXxxInput, UpdateXxxRequest → UpdateXxxInput.
func BuildRequestValidationMap(requests []RequestDef) RequestValidationMap {
	m := make(RequestValidationMap)
	for _, req := range requests {
		// CreateUserRequest → CreateUserInput
		inputName := strings.TrimSuffix(req.Name, "Request") + "Input"
		for _, f := range req.Fields {
			if f.Validate != "" {
				m[inputName+"."+f.Name] = f.Validate
			}
		}
	}
	return m
}

// EnumDef represents a GraphQL enum extracted from oneof= validation.
type EnumDef struct {
	Name   string   // e.g. "PostStatus"
	Values []string // e.g. ["DRAFT", "PUBLISHED", "ARCHIVED"]
}

// ExtractEnums parses oneof= validation tags from request fields and returns
// enum definitions. The enum name is derived from the struct + field name.
// For example: UpdatePostRequest.Status with oneof=draft published archived
// → enum PostStatus { DRAFT PUBLISHED ARCHIVED }
func ExtractEnums(requests []RequestDef) []EnumDef {
	seen := map[string]bool{}
	var enums []EnumDef

	for _, req := range requests {
		// Extract resource name: CreatePostRequest → Post, UpdatePostRequest → Post
		resource := req.Name
		resource = strings.TrimPrefix(resource, "Create")
		resource = strings.TrimPrefix(resource, "Update")
		resource = strings.TrimSuffix(resource, "Request")

		for _, f := range req.Fields {
			oneofVals := extractOneof(f.Validate)
			if len(oneofVals) == 0 {
				continue
			}
			enumName := resource + f.Name
			if seen[enumName] {
				continue
			}
			seen[enumName] = true

			values := make([]string, len(oneofVals))
			for i, v := range oneofVals {
				values[i] = strings.ToUpper(v)
			}
			enums = append(enums, EnumDef{Name: enumName, Values: values})
		}
	}
	return enums
}

// EnumFieldMap maps input field keys ("CreatePostInput.Status") to enum type names ("PostStatus").
func EnumFieldMap(requests []RequestDef) map[string]string {
	m := make(map[string]string)
	for _, req := range requests {
		resource := req.Name
		resource = strings.TrimPrefix(resource, "Create")
		resource = strings.TrimPrefix(resource, "Update")
		resource = strings.TrimSuffix(resource, "Request")

		inputName := strings.TrimSuffix(req.Name, "Request") + "Input"

		for _, f := range req.Fields {
			oneofVals := extractOneof(f.Validate)
			if len(oneofVals) == 0 {
				continue
			}
			enumName := resource + f.Name
			m[inputName+"."+f.Name] = enumName
		}
	}
	return m
}

// extractOneof parses the oneof= value from a validate tag string.
// e.g. "required,oneof=draft published archived" → ["draft", "published", "archived"]
func extractOneof(validate string) []string {
	for _, part := range strings.Split(validate, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "oneof=") {
			vals := strings.TrimPrefix(part, "oneof=")
			return strings.Fields(vals)
		}
	}
	return nil
}
