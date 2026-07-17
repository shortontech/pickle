package generator

import (
	"go/format"
	"regexp"
)

var graphQLModelQueryCall = regexp.MustCompile(`models\.Query[A-Za-z0-9_]+\(\)`)

func formatGraphQLWithPolicyContext(source []byte) ([]byte, error) {
	return formatGraphQLWithPolicyContextExpression(source, `ctx.PolicyContext().(models.PolicyContext)`)
}

func formatGraphQLWithPolicyContextExpression(source []byte, expression string) ([]byte, error) {
	source = graphQLModelQueryCall.ReplaceAllFunc(source, func(call []byte) []byte {
		return []byte(string(call) + `.ApplyPolicyContext(` + expression + `)`)
	})
	return format.Source(source)
}
