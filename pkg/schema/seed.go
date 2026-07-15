package schema

import (
	"fmt"
	"strconv"
	"strings"
)

// SeedSpec is the serializable fake-data declaration attached to a column.
// It is schema metadata only and never changes database DDL.
type SeedSpec struct {
	Kind       string   `json:"kind"`
	Arguments  []string `json:"arguments,omitempty"`
	Fields     []string `json:"fields,omitempty"`
	Reference  string   `json:"reference,omitempty"`
	NullWeight float64  `json:"null_weight,omitempty"`
}

// SeedLocale and SeedCountry keep provider arguments typed in migrations.
type SeedLocale string
type SeedCountry string

const (
	EnUS SeedLocale = "en-US"
	EnCA SeedLocale = "en-CA"

	UnitedStates SeedCountry = "US"
	Canada       SeedCountry = "CA"
)

// SeederRef names a custom value seeder and its declared logical return type.
// Seeder discovery in the generator produces these tokens for migration use.
type SeederRef struct {
	Name       string
	ReturnType ColumnType
	Table      string
}

func NewSeederRef(name string, returnType ColumnType) SeederRef {
	if strings.TrimSpace(name) == "" {
		panic("pickle: seeder reference name must not be empty")
	}
	return SeederRef{Name: name, ReturnType: returnType}
}

func (c *Column) setSeed(kind string, args ...string) *Column {
	c.Seeder = &SeedSpec{Kind: kind, Arguments: append([]string(nil), args...)}
	return c
}

func (c *Column) requireSeedTypes(method string, allowed ...ColumnType) {
	if c.Type < UUID { // metadata alteration; validated against resolved schema during inspection
		return
	}
	for _, typ := range allowed {
		if c.Type == typ {
			return
		}
	}
	panic(fmt.Sprintf("pickle: %s is incompatible with %s column %q", method, c.Type, c.Name))
}

func nonemptySeedArgs(method string, values []string) {
	if len(values) == 0 {
		panic("pickle: " + method + " requires at least one value")
	}
	for _, value := range values {
		if value == "" {
			panic("pickle: " + method + " values must not be empty")
		}
	}
}

func (c *Column) SeedValue(value any) *Column {
	return c.setSeed("value", fmt.Sprint(value))
}

func (c *Column) SeedValues(values ...string) *Column {
	c.requireSeedTypes("SeedValues", String, Text, Time)
	nonemptySeedArgs("SeedValues", values)
	return c.setSeed("values", values...)
}

func (c *Column) SeedRandomString(length int) *Column {
	c.requireSeedTypes("SeedRandomString", String, Text)
	if length < 1 || (c.Length > 0 && length > c.Length) {
		panic("pickle: SeedRandomString length must fit the destination column")
	}
	return c.setSeed("random_string", strconv.Itoa(length))
}

func (c *Column) SeedRandomStringIn(values ...string) *Column {
	c.requireSeedTypes("SeedRandomStringIn", String, Text)
	nonemptySeedArgs("SeedRandomStringIn", values)
	for _, value := range values {
		if c.Length > 0 && len(value) > c.Length {
			panic("pickle: SeedRandomStringIn value exceeds destination column length")
		}
	}
	return c.setSeed("random_string_in", values...)
}

func (c *Column) SeedInteger(min, max int) *Column {
	c.requireSeedTypes("SeedInteger", Integer)
	if min > max {
		panic("pickle: SeedInteger minimum exceeds maximum")
	}
	return c.setSeed("integer", strconv.Itoa(min), strconv.Itoa(max))
}

func (c *Column) SeedBigInteger(min, max int64) *Column {
	c.requireSeedTypes("SeedBigInteger", BigInteger)
	if min > max {
		panic("pickle: SeedBigInteger minimum exceeds maximum")
	}
	return c.setSeed("big_integer", strconv.FormatInt(min, 10), strconv.FormatInt(max, 10))
}

func (c *Column) SeedDecimal(min, max string, scale int) *Column {
	c.requireSeedTypes("SeedDecimal", Decimal)
	if scale < 0 || (c.Scale > 0 && scale != c.Scale) {
		panic("pickle: SeedDecimal scale must match the destination column")
	}
	return c.setSeed("decimal", min, max, strconv.Itoa(scale))
}

func (c *Column) SeedBoolean() *Column {
	c.requireSeedTypes("SeedBoolean", Boolean)
	return c.setSeed("boolean")
}

func (c *Column) SeedBooleanWeighted(trueWeight float64) *Column {
	c.requireSeedTypes("SeedBooleanWeighted", Boolean)
	if trueWeight < 0 || trueWeight > 1 {
		panic("pickle: boolean seed weight must be between 0 and 1")
	}
	return c.setSeed("boolean_weighted", strconv.FormatFloat(trueWeight, 'g', -1, 64))
}

func (c *Column) SeedUUID() *Column { c.requireSeedTypes("SeedUUID", UUID); return c.setSeed("uuid") }
func (c *Column) SeedBytes(length int) *Column {
	c.requireSeedTypes("SeedBytes", Binary)
	if length < 1 {
		panic("pickle: SeedBytes length must be positive")
	}
	return c.setSeed("bytes", strconv.Itoa(length))
}

func (c *Column) SeedJSON(ref SeederRef) *Column {
	c.requireSeedTypes("SeedJSON", JSONB)
	if ref.Name == "" || ref.ReturnType != JSONB {
		panic("pickle: SeedJSON requires a JSONB-compatible custom seeder")
	}
	c.Seeder = &SeedSpec{Kind: "json", Reference: ref.Name}
	return c
}

func (c *Column) seedStringProvider(kind, method string, args ...string) *Column {
	c.requireSeedTypes(method, String, Text)
	return c.setSeed(kind, args...)
}

func locales(values []SeedLocale) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}
func countries(values []SeedCountry) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

func (c *Column) SeedFirstName(values ...SeedLocale) *Column {
	return c.seedStringProvider("first_name", "SeedFirstName", locales(values)...)
}
func (c *Column) SeedLastName(values ...SeedLocale) *Column {
	return c.seedStringProvider("last_name", "SeedLastName", locales(values)...)
}
func (c *Column) SeedFullName(values ...SeedLocale) *Column {
	return c.seedStringProvider("full_name", "SeedFullName", locales(values)...)
}
func (c *Column) SeedUsername() *Column { return c.seedStringProvider("username", "SeedUsername") }
func (c *Column) SeedJobTitle() *Column { return c.seedStringProvider("job_title", "SeedJobTitle") }
func (c *Column) SeedDepartment() *Column {
	return c.seedStringProvider("department", "SeedDepartment")
}
func (c *Column) SeedCompanyName() *Column {
	return c.seedStringProvider("company_name", "SeedCompanyName")
}
func (c *Column) SeedCompanySuffix() *Column {
	return c.seedStringProvider("company_suffix", "SeedCompanySuffix")
}
func (c *Column) SeedIndustry() *Column  { return c.seedStringProvider("industry", "SeedIndustry") }
func (c *Column) SeedEmail() *Column     { return c.seedStringProvider("email", "SeedEmail") }
func (c *Column) SeedSafeEmail() *Column { return c.seedStringProvider("safe_email", "SeedSafeEmail") }
func (c *Column) SeedDomainName() *Column {
	return c.seedStringProvider("domain_name", "SeedDomainName")
}
func (c *Column) SeedURL() *Column  { return c.seedStringProvider("url", "SeedURL") }
func (c *Column) SeedIPv4() *Column { return c.seedStringProvider("ipv4", "SeedIPv4") }
func (c *Column) SeedIPv6() *Column { return c.seedStringProvider("ipv6", "SeedIPv6") }
func (c *Column) SeedUserAgent() *Column {
	return c.seedStringProvider("user_agent", "SeedUserAgent")
}
func (c *Column) SeedPhoneNumber(values ...SeedCountry) *Column {
	return c.seedStringProvider("phone_number", "SeedPhoneNumber", countries(values)...)
}
func (c *Column) SeedStreetAddress(values ...SeedCountry) *Column {
	return c.seedStringProvider("street_address", "SeedStreetAddress", countries(values)...)
}
func (c *Column) SeedCity(values ...SeedCountry) *Column {
	return c.seedStringProvider("city", "SeedCity", countries(values)...)
}
func (c *Column) SeedState(values ...SeedCountry) *Column {
	return c.seedStringProvider("state", "SeedState", countries(values)...)
}
func (c *Column) SeedPostalCode(values ...SeedCountry) *Column {
	return c.seedStringProvider("postal_code", "SeedPostalCode", countries(values)...)
}
func (c *Column) SeedCountry() *Column { return c.seedStringProvider("country", "SeedCountry") }
func (c *Column) SeedCountryCode() *Column {
	return c.seedStringProvider("country_code", "SeedCountryCode")
}
func (c *Column) SeedLocale() *Column   { return c.seedStringProvider("locale", "SeedLocale") }
func (c *Column) SeedTimeZone() *Column { return c.seedStringProvider("time_zone", "SeedTimeZone") }
func (c *Column) SeedDateBetween(start, end string) *Column {
	c.requireSeedTypes("SeedDateBetween", Date, Timestamp)
	if start == "" || end == "" {
		panic("pickle: SeedDateBetween requires non-empty bounds")
	}
	return c.setSeed("date_between", start, end)
}
func (c *Column) SeedTimeBetween(start, end string) *Column {
	c.requireSeedTypes("SeedTimeBetween", Time, Timestamp)
	if start == "" || end == "" {
		panic("pickle: SeedTimeBetween requires non-empty bounds")
	}
	return c.setSeed("time_between", start, end)
}
func (c *Column) SeedPastTime(maxAge string) *Column {
	c.requireSeedTypes("SeedPastTime", Timestamp, Date)
	if maxAge == "" {
		panic("pickle: SeedPastTime requires a maximum age")
	}
	return c.setSeed("past_time", maxAge)
}
func (c *Column) SeedFutureTime(maxDistance string) *Column {
	c.requireSeedTypes("SeedFutureTime", Timestamp, Date)
	if maxDistance == "" {
		panic("pickle: SeedFutureTime requires a maximum distance")
	}
	return c.setSeed("future_time", maxDistance)
}
func (c *Column) SeedSentence(words int) *Column {
	if words < 1 {
		panic("pickle: SeedSentence word count must be positive")
	}
	return c.seedStringProvider("sentence", "SeedSentence", strconv.Itoa(words))
}
func (c *Column) SeedParagraph(sentences int) *Column {
	if sentences < 1 {
		panic("pickle: SeedParagraph sentence count must be positive")
	}
	return c.seedStringProvider("paragraph", "SeedParagraph", strconv.Itoa(sentences))
}
func (c *Column) SeedWords(count int) *Column {
	if count < 1 {
		panic("pickle: SeedWords count must be positive")
	}
	return c.seedStringProvider("words", "SeedWords", strconv.Itoa(count))
}
func (c *Column) SeedProductName() *Column {
	return c.seedStringProvider("product_name", "SeedProductName")
}
func (c *Column) SeedCurrencyCode() *Column {
	return c.seedStringProvider("currency_code", "SeedCurrencyCode")
}
func (c *Column) SeedMoney(min, max string) *Column {
	c.requireSeedTypes("SeedMoney", Decimal)
	return c.setSeed("money", min, max)
}

func (c *Column) SeedPassword(fields []string) *Column {
	c.requireSeedTypes("SeedPassword", String, Text)
	nonemptySeedArgs("SeedPassword", fields)
	seen := map[string]bool{}
	for _, field := range fields {
		if field == c.Name {
			panic("pickle: SeedPassword cannot reference itself")
		}
		if seen[field] {
			panic("pickle: SeedPassword contains a duplicate field")
		}
		seen[field] = true
	}
	c.Seeder = &SeedSpec{Kind: "password", Fields: append([]string(nil), fields...)}
	return c
}

func (c *Column) Seed(ref SeederRef) *Column {
	if ref.Name == "" {
		panic("pickle: custom seeder reference must not be empty")
	}
	if c.Type >= UUID && ref.ReturnType != c.Type {
		panic(fmt.Sprintf("pickle: seeder %s returns %s, incompatible with %s", ref.Name, ref.ReturnType, c.Type))
	}
	c.Seeder = &SeedSpec{Kind: "custom", Reference: ref.Name}
	return c
}

func (c *Column) SeedNull(weight float64) *Column {
	if !c.IsNullable {
		panic("pickle: SeedNull requires a nullable column")
	}
	if c.Seeder == nil {
		panic("pickle: SeedNull requires a field seeder")
	}
	if weight < 0 || weight > 1 {
		panic("pickle: null seed weight must be between 0 and 1")
	}
	c.Seeder.NullWeight = weight
	return c
}

func (c *Column) DropSeeder() *Column { c.Seeder = &SeedSpec{Kind: "none"}; return c }

func validateTableSeeders(table *Table) {
	columns := make(map[string]*Column, len(table.Columns))
	for _, column := range table.Columns {
		columns[column.Name] = column
	}
	for _, column := range table.Columns {
		if column.Seeder == nil || column.Seeder.Kind != "password" {
			continue
		}
		for _, field := range column.Seeder.Fields {
			referenced := columns[field]
			if referenced == nil {
				panic(fmt.Sprintf("pickle: SeedPassword on %s.%s references unknown column %q", table.Name, column.Name, field))
			}
			if referenced.Type == JSONB || referenced.Type == Binary {
				panic(fmt.Sprintf("pickle: SeedPassword field %s.%s is not scalar text", table.Name, field))
			}
		}
	}

	state := map[string]int{}
	var visit func(string)
	visit = func(name string) {
		if state[name] == 1 {
			panic(fmt.Sprintf("pickle: seed field dependency cycle on %s.%s", table.Name, name))
		}
		if state[name] == 2 {
			return
		}
		state[name] = 1
		column := columns[name]
		if column != nil && column.Seeder != nil && column.Seeder.Kind == "password" {
			for _, dependency := range column.Seeder.Fields {
				visit(dependency)
			}
		}
		state[name] = 2
	}
	for name := range columns {
		visit(name)
	}
}
