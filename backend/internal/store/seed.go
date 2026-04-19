package store

// SeedIfEmpty used to insert a large pile of mock/demo rows (fake agents,
// fake tasks, fake tools, fake pipelines, fake feed events…) so that every
// dashboard page had something to render on first boot. That was useful
// while building the UI, but it also meant the "real vs fake" line was
// blurry — users would see rows they never created.
//
// It's now deliberately empty. Every table starts empty and is filled by
// real activity: projects come from SeedProjectsIfEmpty (user's folder
// paths), agents / tasks / tools / feed events / logs are written by
// actual backend services as things happen. Legacy mock rows that exist
// from previous boots are wiped by the `wipe_mock_seed_data` migration
// (see migrations.go). This function stays only to keep the call site
// in main.go stable.
func (s *Store) SeedIfEmpty() error {
	return nil
}
