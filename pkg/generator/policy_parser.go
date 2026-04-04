package generator

// NonManagesRoleAnnotations derives RoleAnnotation entries for all non-Manages roles
// from a policy directory. This is the bridge between policy parsing and column
// annotation generation.
func NonManagesRoleAnnotations(policiesDir string) ([]RoleAnnotation, error) {
	policies, err := ParsePolicyOps(policiesDir)
	if err != nil {
		return nil, err
	}
	if len(policies) == 0 {
		return nil, nil
	}

	roles := StaticDeriveRoles(policies)

	var annotations []RoleAnnotation
	for _, r := range roles {
		if r.IsManages {
			continue
		}
		annotations = append(annotations, RoleAnnotation{
			Slug:       r.Slug,
			PascalName: SlugToPascal(r.Slug),
		})
	}
	return annotations, nil
}
