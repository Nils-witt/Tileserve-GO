package store

// EventPublisher receives notifications of domain events as they're
// durably persisted, so an external system (e.g. an MQTT broker — see
// internal/events) can react to data changes without polling the API. Every
// method must return promptly and handle its own delivery failures — a
// notification problem must never fail or block the mutation that triggered
// it, so Store treats these calls as fire-and-forget.
type EventPublisher interface {
	MapCreated(m MapRecord)
	MapVersionUpdated(m MapRecord)
	GeoObjectCreated(g GeoObjectRecord)
	GeoObjectUpdated(g GeoObjectRecord)
}

// SetEventPublisher wires p into the store; every relevant mutation notifies
// it after the change is committed. It is not safe to call concurrently with
// the mutation methods below — set it once during startup, before the store
// is handed to the HTTP server. Passing nil disables event publishing (the
// default).
func (s *Store) SetEventPublisher(p EventPublisher) {
	s.events = p
}
