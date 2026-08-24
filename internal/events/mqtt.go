// Package events publishes tileserve-go domain events (new maps, new/updated
// map versions, new/updated geo objects) to an MQTT broker, so external
// systems can react to data changes without polling the API. Publisher
// implements store.EventPublisher; wiring it in is entirely optional (see
// cmd/tileserve-go for the -mqtt-* flags that enable it).
package events

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"nilswitt.dev/tileserve-go/internal/store"
)

// connectTimeout bounds how long NewPublisher waits for the initial
// connection to the broker before giving up.
const connectTimeout = 10 * time.Second

// defaultTopicPrefix is used when Config.TopicPrefix is empty.
const defaultTopicPrefix = "tileserve"

// defaultClientID is used when Config.ClientID is empty.
const defaultClientID = "tileserve-go"

// Config holds the settings needed to connect a Publisher to a broker.
type Config struct {
	// BrokerURL is the broker to connect to, e.g. "tcp://localhost:1883" or
	// "ssl://mqtt.example.com:8883".
	BrokerURL string
	// ClientID identifies this connection to the broker. Defaults to
	// "tileserve-go" if empty; set explicitly when running more than one
	// tileserve-go instance against the same broker, since two clients
	// sharing an id will repeatedly disconnect each other.
	ClientID string
	// Username and Password authenticate to the broker. Left unset if
	// Username is empty.
	Username string
	Password string
	// TopicPrefix is prepended to every published topic. Defaults to
	// "tileserve" if empty.
	TopicPrefix string
}

// Publisher publishes domain events as JSON messages to an MQTT broker. It
// implements store.EventPublisher. The zero value is not usable; construct
// one with NewPublisher.
type Publisher struct {
	client mqtt.Client
	prefix string
}

// NewPublisher connects to the broker described by cfg and returns a
// Publisher. It blocks until the initial connection succeeds or
// connectTimeout elapses.
func NewPublisher(cfg Config) (*Publisher, error) {
	clientID := cfg.ClientID
	if clientID == "" {
		clientID = defaultClientID
	}

	prefix := cfg.TopicPrefix
	if prefix == "" {
		prefix = defaultTopicPrefix
	}

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectTimeout(connectTimeout).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			log.Printf("mqtt: connection to %s lost: %v", cfg.BrokerURL, err)
		})

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}

	client := mqtt.NewClient(opts)

	token := client.Connect()
	if !token.WaitTimeout(connectTimeout) {
		return nil, fmt.Errorf("connect to mqtt broker %s: timed out after %s", cfg.BrokerURL, connectTimeout)
	}

	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("connect to mqtt broker %s: %w", cfg.BrokerURL, err)
	}

	return &Publisher{client: client, prefix: prefix}, nil
}

// Close disconnects from the broker, allowing up to 250ms for any in-flight
// publish to be flushed first.
func (p *Publisher) Close() {
	p.client.Disconnect(250)
}

// publish JSON-encodes payload and publishes it at QoS 1 (not retained) to
// prefix/topic. Delivery happens asynchronously; a marshal or delivery
// failure is logged, not returned, since a broker hiccup must never fail the
// database mutation that triggered this event (see store.EventPublisher).
func (p *Publisher) publish(topic string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("mqtt: marshal event for %s: %v", topic, err)
		return
	}

	token := p.client.Publish(p.prefix+"/"+topic, 1, false, body)

	go func() {
		token.Wait()

		if err := token.Error(); err != nil {
			log.Printf("mqtt: publish to %s: %v", topic, err)
		}
	}()
}

// mapTopic builds a "maps/{uuid}/{suffix}" topic for a map-scoped event.
func mapTopic(mapUUID fmt.Stringer, suffix string) string {
	return "maps/" + mapUUID.String() + "/" + suffix
}

// MapCreated publishes a "maps/{uuid}/created" event for a newly created map.
func (p *Publisher) MapCreated(m store.MapRecord) {
	p.publish(mapTopic(m.UUID, "created"), m)
}

// MapVersionUpdated publishes a "maps/{uuid}/version-updated" event whenever
// a map's current version is bumped to a newly uploaded version.
func (p *Publisher) MapVersionUpdated(m store.MapRecord) {
	p.publish(mapTopic(m.UUID, "version-updated"), m)
}

// GeoObjectCreated publishes a "maps/{uuid}/geo-objects/{uuid}/created"
// event.
func (p *Publisher) GeoObjectCreated(g store.GeoObjectRecord) {
	p.publish(mapTopic(g.MapUUID, "geo-objects/"+g.UUID.String()+"/created"), g)
}

// GeoObjectUpdated publishes a "maps/{uuid}/geo-objects/{uuid}/updated"
// event.
func (p *Publisher) GeoObjectUpdated(g store.GeoObjectRecord) {
	p.publish(mapTopic(g.MapUUID, "geo-objects/"+g.UUID.String()+"/updated"), g)
}
