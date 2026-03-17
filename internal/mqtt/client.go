package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"adird.id/vidi/internal/config"
	"adird.id/vidi/internal/shared"
)

// Client handles all MQTT communication for VIDI backend.
// Subscribes to: driver tracking, offer responses, driver status.
// Publishes to: offers, trip status, trip location.
type Client struct {
	client   pahomqtt.Client
	redis    *redis.Client
	cfg      config.MQTTConfig

	// offerChannels routes incoming offer responses to waiting dispatch goroutines
	// key: "{driverID}:{orderID}"
	offerChannels map[string]chan bool
	offerMu       chan struct{} // mutex via channel pattern
}

func NewClient(cfg config.MQTTConfig, rdb *redis.Client) (*Client, error) {
	c := &Client{
		redis:         rdb,
		cfg:           cfg,
		offerChannels: make(map[string]chan bool),
		offerMu:       make(chan struct{}, 1),
	}
	c.offerMu <- struct{}{}

	opts := pahomqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(cfg.ClientID).
		SetCleanSession(cfg.CleanSession).
		SetAutoReconnect(true).
		SetResumeSubs(true).
		SetMaxReconnectInterval(30 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetOnConnectHandler(func(client pahomqtt.Client) {
			log.Info().Str("broker", cfg.BrokerURL).Msg("MQTT connected")
			// Run in goroutine — paho's internal state isn't fully ready inside OnConnectHandler
			go c.resubscribe(client)
		}).
		SetConnectionLostHandler(func(client pahomqtt.Client, err error) {
			log.Warn().Err(err).Msg("MQTT connection lost — will reconnect")
		})

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username).SetPassword(cfg.Password)
	}

	c.client = pahomqtt.NewClient(opts)

	token := c.client.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		return nil, fmt.Errorf("MQTT connect timeout")
	}
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("MQTT connect: %w", err)
	}

	return c, nil
}

// resubscribe is called on every (re)connect to restore subscriptions.
func (c *Client) resubscribe(client pahomqtt.Client) {
	subs := map[string]pahomqtt.MessageHandler{
		"adird/tracking/driver/+": c.onDriverTracking,   // QoS 0
		"adird/offer/+/response":  c.onOfferResponse,    // QoS 1
		"adird/driver/+/status":   c.onDriverStatus,     // QoS 1
	}
	for topic, handler := range subs {
		qos := byte(0)
		if strings.Contains(topic, "offer") || strings.Contains(topic, "status") {
			qos = 1
		}
		if token := client.Subscribe(topic, qos, handler); token.Wait() && token.Error() != nil {
			log.Error().Err(token.Error()).Str("topic", topic).Msg("MQTT subscribe failed")
		} else {
			log.Debug().Str("topic", topic).Msg("MQTT subscribed")
		}
	}
}

// ─── Handlers (inbound) ───────────────────────────────────────────

func (c *Client) onDriverTracking(_ pahomqtt.Client, msg pahomqtt.Message) {
	// Topic: adird/tracking/driver/{driver_id}
	parts := strings.Split(msg.Topic(), "/")
	if len(parts) != 4 {
		return
	}
	driverID := parts[3]

	var gps shared.GPSPayload
	if err := json.Unmarshal(msg.Payload(), &gps); err != nil {
		log.Warn().Err(err).Str("driver", driverID).Msg("invalid GPS payload")
		return
	}

	// Basic sanity check
	if gps.Speed > 200 || gps.Lat == 0 || gps.Lng == 0 {
		return
	}

	ctx := context.Background()

	// Update Redis GEO index (for dispatch nearest-driver search)
	c.redis.GeoAdd(ctx, "drivers:online", &redis.GeoLocation{
		Name:      driverID,
		Longitude: gps.Lng,
		Latitude:  gps.Lat,
	})

	// Update driver metadata hash (60s TTL = auto-offline if no GPS heartbeat)
	c.redis.HSet(ctx, "driver:"+driverID,
		"lat", gps.Lat,
		"lng", gps.Lng,
		"speed", gps.Speed,
		"heading", gps.Heading,
		"updated_at", gps.Ts,
	)
	c.redis.Expire(ctx, "driver:"+driverID, 60*time.Second)

	// If driver is on a trip, forward location to passenger
	tripID, err := c.redis.HGet(ctx, "driver:"+driverID, "active_trip_id").Result()
	if err == nil && tripID != "" {
		c.PublishTripLocation(tripID, gps)
	}
}

func (c *Client) onOfferResponse(_ pahomqtt.Client, msg pahomqtt.Message) {
	// Topic: adird/offer/{driver_id}/response
	parts := strings.Split(msg.Topic(), "/")
	if len(parts) != 4 {
		return
	}
	driverID := parts[2]

	var resp shared.OfferResponse
	if err := json.Unmarshal(msg.Payload(), &resp); err != nil {
		log.Warn().Err(err).Str("driver", driverID).Msg("invalid offer response")
		return
	}

	log.Info().
		Str("driver", driverID).
		Str("order", resp.OrderID).
		Bool("accepted", resp.Accepted).
		Msg("offer response received")

	// Route to the waiting dispatch goroutine
	key := driverID + ":" + resp.OrderID
	<-c.offerMu
	ch, exists := c.offerChannels[key]
	c.offerMu <- struct{}{}

	if exists {
		ch <- resp.Accepted
	}
}

func (c *Client) onDriverStatus(_ pahomqtt.Client, msg pahomqtt.Message) {
	// Topic: adird/driver/{driver_id}/status
	// Also triggered as LWT when connection drops unexpectedly
	parts := strings.Split(msg.Topic(), "/")
	if len(parts) != 4 {
		return
	}
	driverID := parts[2]

	var payload struct {
		Status string `json:"status"`
		Ts     int64  `json:"ts"`
	}
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		return
	}

	log.Info().Str("driver", driverID).Str("status", payload.Status).Msg("driver status change")

	ctx := context.Background()
	if payload.Status == "offline" {
		// Remove from available drivers GEO set immediately
		c.redis.ZRem(ctx, "drivers:online", driverID)
		c.redis.HSet(ctx, "driver:"+driverID, "status", "offline")
	} else if payload.Status == "online" {
		c.redis.HSet(ctx, "driver:"+driverID, "status", "online")
	}
}

// ─── Publishers (outbound) ────────────────────────────────────────

// SendOffer publishes a ride offer to a specific driver (QoS 1).
func (c *Client) SendOffer(driverID string, offer shared.OfferPayload) error {
	topic := "adird/offer/" + driverID
	data, err := json.Marshal(offer)
	if err != nil {
		return fmt.Errorf("marshal offer: %w", err)
	}

	token := c.client.Publish(topic, 1, false, data) // QoS 1, not retained
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("MQTT publish offer timeout")
	}
	return token.Error()
}

// PublishTripStatus sends trip status update to passenger (QoS 1, retained).
func (c *Client) PublishTripStatus(tripID string, status shared.TripStatusPayload) {
	topic := "adird/trip/" + tripID + "/status"
	data, _ := json.Marshal(status)
	c.client.Publish(topic, 1, true, data) // retained = passenger gets last status on subscribe
}

// PublishTripLocation forwards driver GPS to passenger (QoS 0).
func (c *Client) PublishTripLocation(tripID string, gps shared.GPSPayload) {
	topic := "adird/trip/" + tripID + "/location"
	payload := shared.TripLocationPayload{
		Lat:     gps.Lat,
		Lng:     gps.Lng,
		Speed:   gps.Speed,
		Heading: gps.Heading,
	}
	data, _ := json.Marshal(payload)
	c.client.Publish(topic, 0, false, data)
}

// PublishZoneSurge broadcasts surge multiplier for a zone (QoS 0, retained).
func (c *Client) PublishZoneSurge(zoneID string, multiplier float64) {
	topic := "adird/zone/" + zoneID + "/surge"
	data, _ := json.Marshal(map[string]interface{}{
		"zone_id":    zoneID,
		"multiplier": multiplier,
	})
	c.client.Publish(topic, 0, true, data)
}

// ─── Dispatch Integration ────────────────────────────────────────

// RegisterOfferChannel registers a channel to receive the driver's offer response.
// Called by dispatch engine before publishing the offer.
func (c *Client) RegisterOfferChannel(driverID, orderID string) chan bool {
	key := driverID + ":" + orderID
	ch := make(chan bool, 1)
	<-c.offerMu
	c.offerChannels[key] = ch
	c.offerMu <- struct{}{}
	return ch
}

// UnregisterOfferChannel cleans up after offer is resolved or timed out.
func (c *Client) UnregisterOfferChannel(driverID, orderID string) {
	key := driverID + ":" + orderID
	<-c.offerMu
	delete(c.offerChannels, key)
	c.offerMu <- struct{}{}
}

func (c *Client) Disconnect() {
	c.client.Disconnect(500)
}
