package schema

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// SeedValueContext identifies one generated field. Each field receives its own
// stable random stream, so adding an unrelated field does not reshuffle rows.
type SeedValueContext struct {
	RootSeed   int64
	Scenario   string
	NodePath   string
	RowOrdinal int
	Column     string
	Retry      int
}

// SeedValue generates one application-supplied value from migration metadata.
// Password seeders are resolved by GenerateSeedRow because they depend on
// other fields in the same row.
func SeedValue(spec *SeedSpec, ctx SeedValueContext) (any, error) {
	if spec == nil || spec.Kind == "none" {
		return nil, fmt.Errorf("no field seeder declared")
	}
	r := newSeedStream(ctx)
	if spec.NullWeight > 0 && r.fraction() < spec.NullWeight {
		return nil, nil
	}
	arg := func(index int) (string, error) {
		if index >= len(spec.Arguments) {
			return "", fmt.Errorf("seeder %s is missing argument %d", spec.Kind, index+1)
		}
		return spec.Arguments[index], nil
	}
	choose := func(values []string) (any, error) {
		if len(values) == 0 {
			return nil, fmt.Errorf("seeder %s has no choices", spec.Kind)
		}
		return values[r.index(len(values))], nil
	}

	switch spec.Kind {
	case "value":
		return arg(0)
	case "values", "random_string_in":
		return choose(spec.Arguments)
	case "random_string":
		value, err := arg(0)
		if err != nil {
			return nil, err
		}
		length, err := strconv.Atoi(value)
		if err != nil || length < 1 {
			return nil, fmt.Errorf("invalid random string length %q", value)
		}
		const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		var out strings.Builder
		for i := 0; i < length; i++ {
			out.WriteByte(alphabet[r.index(len(alphabet))])
		}
		return out.String(), nil
	case "integer", "big_integer":
		minText, err := arg(0)
		if err != nil {
			return nil, err
		}
		maxText, err := arg(1)
		if err != nil {
			return nil, err
		}
		min, err := strconv.ParseInt(minText, 10, 64)
		if err != nil {
			return nil, err
		}
		max, err := strconv.ParseInt(maxText, 10, 64)
		if err != nil || max < min {
			return nil, fmt.Errorf("invalid integer seed range")
		}
		value := min + int64(r.uint64()%uint64(max-min+1))
		if spec.Kind == "integer" {
			return int(value), nil
		}
		return value, nil
	case "decimal", "money":
		minText, err := arg(0)
		if err != nil {
			return nil, err
		}
		maxText, err := arg(1)
		if err != nil {
			return nil, err
		}
		scale := 2
		if spec.Kind == "decimal" {
			scaleText, err := arg(2)
			if err != nil {
				return nil, err
			}
			scale, err = strconv.Atoi(scaleText)
			if err != nil || scale < 0 {
				return nil, fmt.Errorf("invalid decimal scale %q", scaleText)
			}
		}
		return seedDecimalBetween(minText, maxText, scale, r)
	case "boolean":
		return r.uint64()%2 == 0, nil
	case "boolean_weighted":
		weightText, err := arg(0)
		if err != nil {
			return nil, err
		}
		weight, err := strconv.ParseFloat(weightText, 64)
		if err != nil {
			return nil, err
		}
		return r.fraction() < weight, nil
	case "uuid":
		bytes := r.bytes(16)
		bytes[6] = (bytes[6] & 0x0f) | 0x40
		bytes[8] = (bytes[8] & 0x3f) | 0x80
		return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", bytes[:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:]), nil
	case "bytes":
		lengthText, err := arg(0)
		if err != nil {
			return nil, err
		}
		length, err := strconv.Atoi(lengthText)
		if err != nil || length < 1 {
			return nil, fmt.Errorf("invalid byte seed length")
		}
		return r.bytes(length), nil
	case "first_name":
		return choose([]string{"Ada", "Grace", "Linus", "Margaret", "Ken", "Barbara", "Edsger", "Radia"})
	case "last_name":
		return choose([]string{"Lovelace", "Hopper", "Torvalds", "Hamilton", "Thompson", "Liskov", "Dijkstra", "Perlman"})
	case "full_name":
		first, _ := choose([]string{"Ada", "Grace", "Linus", "Margaret", "Ken", "Barbara"})
		last, _ := choose([]string{"Lovelace", "Hopper", "Torvalds", "Hamilton", "Thompson", "Liskov"})
		return first.(string) + " " + last.(string), nil
	case "email", "safe_email":
		return fmt.Sprintf("user%d@example.test", r.uint64()%1_000_000), nil
	case "username":
		return fmt.Sprintf("user%d", r.uint64()%1_000_000), nil
	case "phone_number":
		return fmt.Sprintf("+1-555-%03d-%04d", r.uint64()%1000, r.uint64()%10000), nil
	case "company_name":
		return choose([]string{"Acme Labs", "Northstar Works", "Copper Systems", "Juniper Studio"})
	case "company_suffix":
		return choose([]string{"LLC", "Inc.", "Ltd.", "Co."})
	case "department":
		return choose([]string{"Engineering", "Sales", "Support", "Operations", "Finance"})
	case "job_title":
		return choose([]string{"Engineer", "Designer", "Manager", "Analyst", "Specialist"})
	case "industry":
		return choose([]string{"Software", "Healthcare", "Finance", "Manufacturing", "Education"})
	case "time_zone":
		return choose([]string{"America/Los_Angeles", "America/New_York", "Europe/London", "Asia/Tokyo"})
	case "country":
		return choose([]string{"United States", "Canada"})
	case "country_code":
		return choose([]string{"US", "CA"})
	case "locale":
		return choose([]string{"en-US", "en-CA"})
	case "domain_name":
		return fmt.Sprintf("example%d.test", r.uint64()%10000), nil
	case "url":
		return fmt.Sprintf("https://example%d.test", r.uint64()%10000), nil
	case "ipv4":
		return fmt.Sprintf("192.0.2.%d", 1+r.uint64()%254), nil
	case "ipv6":
		return fmt.Sprintf("2001:db8::%x", 1+r.uint64()%65535), nil
	case "postal_code":
		return fmt.Sprintf("%05d", r.uint64()%100000), nil
	case "city":
		return choose([]string{"Portland", "Austin", "Toronto", "Seattle", "Boston"})
	case "state":
		return choose([]string{"California", "Oregon", "Texas", "Ontario", "Washington"})
	case "street_address":
		return fmt.Sprintf("%d Pickle Lane", 1+r.uint64()%9999), nil
	case "user_agent":
		return choose([]string{"Mozilla/5.0 (Seed; Linux x86_64) AppleWebKit/537.36", "PickleSeeder/1.0", "Mozilla/5.0 (Seed; Mac OS X) Gecko/20100101"})
	case "product_name":
		adjective, _ := choose([]string{"Compact", "Artisan", "Classic", "Bright", "Durable"})
		noun, _ := choose([]string{"Notebook", "Lantern", "Backpack", "Mug", "Keyboard"})
		return adjective.(string) + " " + noun.(string), nil
	case "words", "sentence", "paragraph":
		countText, err := arg(0)
		if err != nil {
			return nil, err
		}
		count, err := strconv.Atoi(countText)
		if err != nil || count < 1 {
			return nil, fmt.Errorf("invalid %s count", spec.Kind)
		}
		if spec.Kind == "paragraph" {
			sentences := make([]string, count)
			for index := range sentences {
				sentences[index] = seedSentence(8+r.index(8), r)
			}
			return strings.Join(sentences, " "), nil
		}
		if spec.Kind == "sentence" {
			return seedSentence(count, r), nil
		}
		words := make([]string, count)
		for index := range words {
			words[index] = seedWord(r)
		}
		return strings.Join(words, " "), nil
	case "currency_code":
		return choose([]string{"USD", "CAD", "EUR", "GBP"})
	case "date_between", "time_between":
		startText, err := arg(0)
		if err != nil {
			return nil, err
		}
		endText, err := arg(1)
		if err != nil {
			return nil, err
		}
		start, err := parseSeedTime(startText)
		if err != nil {
			return nil, err
		}
		end, err := parseSeedTime(endText)
		if err != nil || end.Before(start) {
			return nil, fmt.Errorf("invalid time seed range")
		}
		span := end.Sub(start)
		if span == 0 {
			return start, nil
		}
		return start.Add(time.Duration(r.uint64() % uint64(span))), nil
	case "past_time", "future_time":
		distanceText, err := arg(0)
		if err != nil {
			return nil, err
		}
		distance, err := time.ParseDuration(distanceText)
		if err != nil || distance <= 0 {
			return nil, fmt.Errorf("invalid time distance %q", distanceText)
		}
		anchor := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
		offset := time.Duration(r.uint64() % uint64(distance))
		if spec.Kind == "past_time" {
			return anchor.Add(-offset), nil
		}
		return anchor.Add(offset), nil
	case "password":
		return nil, fmt.Errorf("password seeders require row context")
	case "custom", "json":
		return nil, fmt.Errorf("custom seeder %q must be invoked by generated application code", spec.Reference)
	default:
		return nil, fmt.Errorf("unsupported field seeder %q", spec.Kind)
	}
}

// GenerateSeedRow resolves independent fields first and ordered composite
// passwords last. The returned password is plaintext; generated SQL execution
// hashes it immediately before insertion.
func GenerateSeedRow(table *Table, overrides map[string]any, base SeedValueContext) (map[string]any, error) {
	return GenerateSeedRowWith(table, overrides, base, nil)
}

// GenerateSeedRowWith additionally resolves custom migration field seeders.
func GenerateSeedRowWith(table *Table, overrides map[string]any, base SeedValueContext, resolver func(string, SeedValueContext) (any, bool, error)) (map[string]any, error) {
	row := make(map[string]any, len(table.Columns))
	for key, value := range overrides {
		row[key] = value
	}
	for _, column := range table.Columns {
		if _, exists := row[column.Name]; exists || column.Seeder == nil || column.Seeder.Kind == "password" {
			continue
		}
		ctx := base
		ctx.Column = column.Name
		var value any
		var err error
		if column.Seeder.Kind == "custom" || column.Seeder.Kind == "json" {
			if resolver == nil {
				return nil, fmt.Errorf("seed %s.%s: custom seeder %q is not registered", table.Name, column.Name, column.Seeder.Reference)
			}
			var found bool
			value, found, err = resolver(column.Seeder.Reference, ctx)
			if err == nil && !found {
				err = fmt.Errorf("custom seeder %q is not registered", column.Seeder.Reference)
			}
		} else {
			value, err = SeedValue(column.Seeder, ctx)
		}
		if err != nil {
			return nil, fmt.Errorf("seed %s.%s: %w", table.Name, column.Name, err)
		}
		value, err = castSeedValue(column.Type, value)
		if err != nil {
			return nil, fmt.Errorf("seed %s.%s: %w", table.Name, column.Name, err)
		}
		row[column.Name] = value
	}
	for _, column := range table.Columns {
		if _, exists := row[column.Name]; exists || column.Seeder == nil || column.Seeder.Kind != "password" {
			continue
		}
		parts := make([]string, len(column.Seeder.Fields))
		for index, field := range column.Seeder.Fields {
			value, exists := row[field]
			if !exists {
				return nil, fmt.Errorf("seed %s.%s: composite field %q has no value", table.Name, column.Name, field)
			}
			parts[index] = seedPasswordPart(value)
		}
		row[column.Name] = strings.ToLower(strings.Join(parts, "-"))
	}
	columns := make(map[string]*Column, len(table.Columns))
	for _, column := range table.Columns {
		columns[column.Name] = column
	}
	for name := range row {
		if columns[name] == nil {
			return nil, fmt.Errorf("seed %s: value supplied for unknown column %q", table.Name, name)
		}
	}
	for _, column := range table.Columns {
		value, exists := row[column.Name]
		if !exists {
			if !column.IsNullable && !column.HasDefault {
				return nil, fmt.Errorf("seed %s.%s: required column has no value source", table.Name, column.Name)
			}
			continue
		}
		cast, err := castSeedValue(column.Type, value)
		if err != nil {
			return nil, fmt.Errorf("seed %s.%s: %w", table.Name, column.Name, err)
		}
		row[column.Name] = cast
	}
	return row, nil
}

func seedPasswordPart(value any) string {
	part := strings.TrimSpace(fmt.Sprint(value))
	part = strings.ToLower(strings.Join(strings.Fields(part), "-"))
	return part
}

func parseSeedTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, time.DateOnly, time.TimeOnly} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid seed time %q", value)
}

func seedDecimalBetween(minText, maxText string, scale int, stream *seedStream) (string, error) {
	min, ok := new(big.Rat).SetString(minText)
	if !ok {
		return "", fmt.Errorf("invalid decimal minimum %q", minText)
	}
	max, ok := new(big.Rat).SetString(maxText)
	if !ok || max.Cmp(min) < 0 {
		return "", fmt.Errorf("invalid decimal maximum %q", maxText)
	}
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	toScaled := func(value *big.Rat) *big.Int {
		scaled := new(big.Rat).Mul(value, new(big.Rat).SetInt(factor))
		return new(big.Int).Quo(scaled.Num(), scaled.Denom())
	}
	minInt, maxInt := toScaled(min), toScaled(max)
	span := new(big.Int).Sub(maxInt, minInt)
	span.Add(span, big.NewInt(1))
	choice := new(big.Int).SetUint64(stream.uint64())
	choice.Mod(choice, span)
	choice.Add(choice, minInt)
	negative := choice.Sign() < 0
	abs := new(big.Int).Abs(choice)
	digits := abs.String()
	for len(digits) <= scale {
		digits = "0" + digits
	}
	if scale > 0 {
		digits = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
	}
	if negative {
		digits = "-" + digits
	}
	return digits, nil
}

var seedWords = []string{"amber", "bridge", "cedar", "delta", "ember", "field", "garden", "harbor", "island", "juniper", "kindle", "lantern", "meadow", "north", "olive", "pepper", "quiet", "river", "salt", "timber"}

func seedWord(stream *seedStream) string { return seedWords[stream.index(len(seedWords))] }

func seedSentence(count int, stream *seedStream) string {
	words := make([]string, count)
	for index := range words {
		words[index] = seedWord(stream)
	}
	words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	return strings.Join(words, " ") + "."
}

func castSeedValue(columnType ColumnType, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	text := fmt.Sprint(value)
	switch columnType {
	case String, Text, UUID:
		return text, nil
	case Integer:
		parsed, err := strconv.Atoi(text)
		if err != nil {
			return nil, fmt.Errorf("cannot cast %q to integer", text)
		}
		return parsed, nil
	case BigInteger:
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot cast %q to big integer", text)
		}
		return parsed, nil
	case Boolean:
		parsed, err := strconv.ParseBool(text)
		if err != nil {
			return nil, fmt.Errorf("cannot cast %q to boolean", text)
		}
		return parsed, nil
	case Decimal:
		if _, ok := new(big.Rat).SetString(text); !ok {
			return nil, fmt.Errorf("cannot cast %q to decimal", text)
		}
		return text, nil
	case Float, Double:
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot cast %q to floating point", text)
		}
		return parsed, nil
	case Date, Timestamp, Time:
		if parsed, ok := value.(time.Time); ok {
			return parsed, nil
		}
		parsed, err := parseSeedTime(text)
		if err != nil {
			return nil, err
		}
		return parsed, nil
	case JSONB:
		switch typed := value.(type) {
		case []byte:
			if !json.Valid(typed) {
				return nil, errors.New("custom JSON seeder returned invalid JSON")
			}
			return typed, nil
		case string:
			if !json.Valid([]byte(typed)) {
				return nil, errors.New("custom JSON seeder returned invalid JSON")
			}
			return []byte(typed), nil
		default:
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("cannot cast custom seeder result to JSON: %w", err)
			}
			return encoded, nil
		}
	case Binary:
		if bytes, ok := value.([]byte); ok {
			return bytes, nil
		}
		return []byte(text), nil
	default:
		return value, nil
	}
}

type seedStream struct {
	key     [32]byte
	counter uint64
}

func newSeedStream(ctx SeedValueContext) *seedStream {
	identity := fmt.Sprintf("pickle-seed-v1\x00%d\x00%s\x00%s\x00%d\x00%s\x00%d", ctx.RootSeed, ctx.Scenario, ctx.NodePath, ctx.RowOrdinal, ctx.Column, ctx.Retry)
	return &seedStream{key: sha256.Sum256([]byte(identity))}
}

func (r *seedStream) uint64() uint64 {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], r.counter)
	r.counter++
	block := sha256.Sum256(append(r.key[:], counter[:]...))
	return binary.BigEndian.Uint64(block[:8])
}
func (r *seedStream) index(length int) int { return int(r.uint64() % uint64(length)) }
func (r *seedStream) fraction() float64    { return float64(r.uint64()>>11) / (1 << 53) }
func (r *seedStream) bytes(length int) []byte {
	out := make([]byte, length)
	for offset := 0; offset < length; {
		value := r.uint64()
		for i := 0; i < 8 && offset < length; i++ {
			out[offset] = byte(value)
			value >>= 8
			offset++
		}
	}
	return out
}
