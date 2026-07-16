package schema

import "strings"

type PolicyIdentityType string

const (
	PolicyIdentityUUID    PolicyIdentityType = "uuid"
	PolicyIdentityString  PolicyIdentityType = "string"
	PolicyIdentityStrings PolicyIdentityType = "strings"
)

type PolicyIdentityDefinition struct {
	Name string
	Type PolicyIdentityType
}

func (p *Policy) IdentityUUID(name string)    { p.declareIdentity(name, PolicyIdentityUUID) }
func (p *Policy) IdentityString(name string)  { p.declareIdentity(name, PolicyIdentityString) }
func (p *Policy) IdentityStrings(name string) { p.declareIdentity(name, PolicyIdentityStrings) }
func (p *Policy) declareIdentity(name string, kind PolicyIdentityType) {
	rowPolicyName("identity", name)
	for _, existing := range p.IdentityDefinitions {
		if existing.Name == name {
			panic("pickle: duplicate policy identity \"" + name + "\"")
		}
	}
	p.IdentityDefinitions = append(p.IdentityDefinitions, PolicyIdentityDefinition{Name: name, Type: kind})
}

// RowPolicyOperation is a replayable row-authorization state transition.
type RowPolicyOperation struct {
	Type       string // protect, alter_protection, unprotect
	Protection RowProtection
}

// RowProtection is the normalized source definition for one protected table.
type RowProtection struct {
	Table                  string
	SubjectCombination     SubjectCombination
	Rules                  []RowRule
	ApplicationOnlyReasons []string
}

type SubjectCombination string

const (
	AnyOfSubjects SubjectCombination = "any"
	AllOfSubjects SubjectCombination = "all"
)

type RowSubjectKind string

const (
	SubjectPublic        RowSubjectKind = "public"
	SubjectAuthenticated RowSubjectKind = "authenticated"
	SubjectRole          RowSubjectKind = "role"
)

type RowSubject struct {
	Kind RowSubjectKind
	Name string
}

type RowRule struct {
	Key       string
	Subject   RowSubject
	Select    *RowPredicate
	Insert    *RowPredicate
	UpdateOld *RowPredicate
	UpdateNew *RowPredicate
	Delete    *RowPredicate
}

type RowPredicateKind string

const (
	PredicateAllow    RowPredicateKind = "allow"
	PredicateDeny     RowPredicateKind = "deny"
	PredicateIdentity RowPredicateKind = "identity"
	PredicateColumn   RowPredicateKind = "column"
	PredicateEqual    RowPredicateKind = "equal"
	PredicateNotEqual RowPredicateKind = "not_equal"
	PredicateAnd      RowPredicateKind = "and"
	PredicateOr       RowPredicateKind = "or"
	PredicateNot      RowPredicateKind = "not"
)

// RowPredicate is a database-independent authorization expression. It never
// contains Go callbacks or raw SQL.
type RowPredicate struct {
	Kind     RowPredicateKind
	Name     string
	Children []RowPredicate
}

func Allow() RowPredicate { return RowPredicate{Kind: PredicateAllow} }
func Deny() RowPredicate  { return RowPredicate{Kind: PredicateDeny} }
func Identity(name string) RowPredicate {
	rowPolicyName("identity", name)
	return RowPredicate{Kind: PredicateIdentity, Name: name}
}
func PolicyColumn(name string) RowPredicate {
	rowPolicyName("column", name)
	return RowPredicate{Kind: PredicateColumn, Name: name}
}
func Equal(left, right RowPredicate) RowPredicate {
	return RowPredicate{Kind: PredicateEqual, Children: []RowPredicate{left, right}}
}
func NotEqual(left, right RowPredicate) RowPredicate {
	return RowPredicate{Kind: PredicateNotEqual, Children: []RowPredicate{left, right}}
}
func Owner(column string, identity RowPredicate) RowPredicate {
	return Equal(PolicyColumn(column), identity)
}
func And(predicates ...RowPredicate) RowPredicate {
	if len(predicates) < 2 {
		panic("pickle: row policy And requires at least two predicates")
	}
	return RowPredicate{Kind: PredicateAnd, Children: append([]RowPredicate(nil), predicates...)}
}
func Or(predicates ...RowPredicate) RowPredicate {
	if len(predicates) < 2 {
		panic("pickle: row policy Or requires at least two predicates")
	}
	return RowPredicate{Kind: PredicateOr, Children: append([]RowPredicate(nil), predicates...)}
}
func Not(predicate RowPredicate) RowPredicate {
	return RowPredicate{Kind: PredicateNot, Children: []RowPredicate{predicate}}
}

type PositionedPredicate struct {
	Position string
	Value    RowPredicate
}

func Existing(predicate RowPredicate) PositionedPredicate {
	return PositionedPredicate{Position: "existing", Value: predicate}
}
func Proposed(predicate RowPredicate) PositionedPredicate {
	return PositionedPredicate{Position: "proposed", Value: predicate}
}

type Rows struct {
	protection *RowProtection
}

type RowRuleBuilder struct {
	protection *RowProtection
	rule       *RowRule
}

func (p *Policy) Protect(table string, configure func(*Rows)) {
	p.addProtection("protect", table, configure)
}

func (p *Policy) AlterProtection(table string, configure func(*Rows)) {
	p.addProtection("alter_protection", table, configure)
}

func (p *Policy) addProtection(kind, table string, configure func(*Rows)) {
	rowPolicyName("table", table)
	protection := RowProtection{Table: table, SubjectCombination: AnyOfSubjects}
	if configure != nil {
		configure(&Rows{protection: &protection})
	}
	validateRowProtection(protection)
	p.RowOperations = append(p.RowOperations, RowPolicyOperation{Type: kind, Protection: protection})
}

func (p *Policy) Unprotect(table string) {
	rowPolicyName("table", table)
	p.RowOperations = append(p.RowOperations, RowPolicyOperation{Type: "unprotect", Protection: RowProtection{Table: table}})
}

func (r *Rows) CombineSubjects(mode SubjectCombination) {
	if mode != AnyOfSubjects && mode != AllOfSubjects {
		panic("pickle: invalid row policy subject combination")
	}
	r.protection.SubjectCombination = mode
}

func (r *Rows) AllowApplicationOnly(reason string) {
	rowPolicyName("application-only reason", reason)
	r.protection.ApplicationOnlyReasons = append(r.protection.ApplicationOnlyReasons, reason)
}

func (r *Rows) Rule(key string) *RowRuleBuilder {
	rowPolicyName("rule key", key)
	r.protection.Rules = append(r.protection.Rules, RowRule{Key: key})
	return &RowRuleBuilder{protection: r.protection, rule: &r.protection.Rules[len(r.protection.Rules)-1]}
}

func (b *RowRuleBuilder) ForPublic() *RowRuleBuilder {
	b.rule.Subject = RowSubject{Kind: SubjectPublic}
	return b
}
func (b *RowRuleBuilder) ForAuthenticated() *RowRuleBuilder {
	b.rule.Subject = RowSubject{Kind: SubjectAuthenticated}
	return b
}
func (b *RowRuleBuilder) ForRole(role string) *RowRuleBuilder {
	rowPolicyName("role", role)
	b.rule.Subject = RowSubject{Kind: SubjectRole, Name: role}
	return b
}
func (b *RowRuleBuilder) Select(predicate RowPredicate) *RowRuleBuilder {
	validateRowPredicate(predicate)
	b.rule.Select = copyRowPredicate(predicate)
	return b
}
func (b *RowRuleBuilder) Insert(predicate RowPredicate) *RowRuleBuilder {
	validateRowPredicate(predicate)
	b.rule.Insert = copyRowPredicate(predicate)
	return b
}
func (b *RowRuleBuilder) Delete(predicate RowPredicate) *RowRuleBuilder {
	validateRowPredicate(predicate)
	b.rule.Delete = copyRowPredicate(predicate)
	return b
}
func (b *RowRuleBuilder) Update(existing, proposed PositionedPredicate) *RowRuleBuilder {
	if existing.Position != "existing" || proposed.Position != "proposed" {
		panic("pickle: Update requires Existing(...) then Proposed(...)")
	}
	validateRowPredicate(existing.Value)
	validateRowPredicate(proposed.Value)
	b.rule.UpdateOld, b.rule.UpdateNew = copyRowPredicate(existing.Value), copyRowPredicate(proposed.Value)
	return b
}
func (b *RowRuleBuilder) All(predicate RowPredicate) *RowRuleBuilder {
	validateRowPredicate(predicate)
	b.rule.Select, b.rule.Insert, b.rule.UpdateOld, b.rule.UpdateNew, b.rule.Delete = copyRowPredicate(predicate), copyRowPredicate(predicate), copyRowPredicate(predicate), copyRowPredicate(predicate), copyRowPredicate(predicate)
	return b
}

func copyRowPredicate(predicate RowPredicate) *RowPredicate {
	copy := predicate
	copy.Children = append([]RowPredicate(nil), predicate.Children...)
	return &copy
}

func rowPolicyName(kind, value string) {
	if strings.TrimSpace(value) == "" || strings.ContainsRune(value, '\x00') {
		panic("pickle: row policy " + kind + " must not be empty or contain NUL")
	}
}

func validateRowProtection(protection RowProtection) {
	seen := map[string]bool{}
	for _, rule := range protection.Rules {
		if seen[rule.Key] {
			panic("pickle: duplicate row policy rule key \"" + rule.Key + "\" on table \"" + protection.Table + "\"")
		}
		seen[rule.Key] = true
		if rule.Subject.Kind == "" {
			panic("pickle: row policy rule \"" + rule.Key + "\" requires a subject")
		}
		if rule.Select == nil && rule.Insert == nil && rule.UpdateOld == nil && rule.Delete == nil {
			panic("pickle: row policy rule \"" + rule.Key + "\" requires an operation")
		}
		if (rule.UpdateOld == nil) != (rule.UpdateNew == nil) {
			panic("pickle: row policy update requires existing and proposed predicates")
		}
	}
}

func validateRowPredicate(predicate RowPredicate) {
	switch predicate.Kind {
	case PredicateAllow, PredicateDeny:
		if len(predicate.Children) != 0 {
			panic("pickle: constant row predicate cannot have children")
		}
	case PredicateIdentity, PredicateColumn:
		rowPolicyName("predicate name", predicate.Name)
		if len(predicate.Children) != 0 {
			panic("pickle: named row predicate cannot have children")
		}
	case PredicateEqual, PredicateNotEqual:
		if len(predicate.Children) != 2 {
			panic("pickle: comparison row predicate requires two children")
		}
	case PredicateAnd, PredicateOr:
		if len(predicate.Children) < 2 {
			panic("pickle: boolean row predicate requires at least two children")
		}
	case PredicateNot:
		if len(predicate.Children) != 1 {
			panic("pickle: Not row predicate requires one child")
		}
	default:
		panic("pickle: unknown row predicate kind")
	}
	for _, child := range predicate.Children {
		validateRowPredicate(child)
	}
}
