package cooked

// NewVerifiedPolicyContext is linked only into package tests. Production
// generated applications construct contexts through sealed entry-point adapters.
func NewVerifiedPolicyContext(identities map[string]string, roles []string) PolicyContext {
	return newVerifiedPolicyContext(identities, roles)
}
