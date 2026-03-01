package migrations

type CreateUserPostStatsView_2026_02_28_100000 struct {
	Migration
}

func (m *CreateUserPostStatsView_2026_02_28_100000) Up() {
	m.CreateView("user_post_stats", func(v *View) {
		v.From("users", "u")
		v.LeftJoin("posts", "p", "p.user_id = u.id")
		v.Column("u.id")
		v.Column("u.name")
		v.Column("u.email")
		v.GroupBy("u.id", "u.name", "u.email")
		v.SelectRaw("post_count", "COUNT(p.id)").BigInteger()
		v.SelectRaw("latest_post_at", "MAX(p.created_at)").TimestampType()
	})
}

func (m *CreateUserPostStatsView_2026_02_28_100000) Down() {
	m.DropView("user_post_stats")
}
