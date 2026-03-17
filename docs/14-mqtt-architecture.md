---
title: MQTT Architecture — EMQX Real-Time Communication
tags: [mqtt, emqx, vidi, vici, real-time, architecture]
created: 2026-03-16
---

# MQTT Architecture — EMQX Real-Time Communication

This document defines the complete MQTT communication layer for ADIRD, replacing the previous WebSocket approach. Real-time messaging runs entirely over MQTT via EMQX. REST API handles all non-real-time operations.

Related: [[02-system-architecture]] | [[05-realtime-tracking]] | [[04-dispatch-algorithm]] | [[15-vini-dashboard]] | [[16-scale-50k]]

---

## 1. Why MQTT over WebSocket

The previous design used raw WebSocket connections managed by VIDI. MQTT over EMQX is a purpose-built replacement for mobile-first real-time messaging.

| Factor | WebSocket (previous) | MQTT + EMQX |
|---|---|---|
| Mobile reconnect | Client must re-register handlers after reconnect | Persistent session (`cleanSession=false`) resumes automatically |
| Disconnect detection | Custom goroutine + timeout logic in VIDI | LWT (Last Will & Testament) built into protocol |
| Packet overhead | 8-byte frame header minimum | 2-byte fixed header minimum |
| Battery (Android) | Keep-alive polling burden on app | Configurable keepalive, broker manages ping |
| QoS for offers | Custom Redis retry + ack logic | QoS 1 guaranteed delivery, built-in |
| Jakarta 3G networks | Connection loss = missed messages | Persistent session + QoS 1 queues messages during disconnect |
| Broker features | Hand-rolled in VIDI Go code | EMQX: auth, ACL, rule engine, dashboard, metrics |
| Go ecosystem | `gorilla/websocket`, custom hub | `paho.mqtt.golang`, standard client |

### Key advantages

**Persistent sessions survive TCP disconnects.** Motorcycle riders on Jakarta roads lose connectivity frequently. With `cleanSession=false`, EMQX holds QoS 1 messages for the client until it reconnects. The driver misses nothing.

**LWT auto-detects offline drivers.** When a TCP connection drops without a clean DISCONNECT packet, EMQX automatically publishes the pre-registered Last Will message to `adird/driver/{driver_id}/status`. VIDI no longer needs a goroutine watching for stale connections.

**QoS 1 for offers eliminates custom retry logic.** The previous implementation required Redis-backed retry loops to ensure offer delivery. EMQX QoS 1 handles this at the protocol level: broker retains the message and retransmits until the client ACKs.

**Binary protocol reduces battery drain.** MQTT fixed header is 2 bytes. WebSocket frame overhead is 8 bytes plus masking. At 7,500 GPS messages/second across 30K drivers, this difference accumulates on the device radio.

**EMQX handles 50K concurrent connections on modest hardware.** A single Hetzner CX42 (8vCPU, 16GB RAM) is sufficient. See [[16-scale-50k]] for capacity analysis.

**EMQX built-in JWT authentication.** No custom auth middleware for connection establishment. EMQX validates the JWT on connect using the RS256 public key.

---

## 2. MQTT Topic Design

All topics are namespaced under `adird/`. Topic segments use snake_case identifiers. No trailing slashes.

### 2.1 Driver Topics (VICI → EMQX → VIDI)

```
# GPS location — driver publishes continuously while online
adird/tracking/driver/{driver_id}

  QoS:      0 (fire-and-forget)
  Retained: false
  Publisher: VICI (driver app)
  Subscriber: VIDI backend, VINI dashboard (wildcard)

  Payload:
  {
    "lat": -6.1754,
    "lng": 106.8272,
    "speed": 35.2,
    "heading": 270,
    "ts": 1710000000
  }
```

```
# Driver online/offline status — also serves as LWT target
adird/driver/{driver_id}/status

  QoS:      1 (guaranteed delivery)
  Retained: true (broker holds last known status)
  Publisher: VICI on connect/disconnect, EMQX LWT on unexpected drop
  Subscriber: VIDI backend

  Payload:
  {
    "status": "online" | "offline",
    "ts": 1710000000
  }
```

### 2.2 Offer Topics (VIDI → EMQX → VICI Driver)

```
# Ride offer delivered to a specific driver
adird/offer/{driver_id}

  QoS:      1 (guaranteed delivery — critical for dispatch)
  Retained: false
  Publisher: VIDI dispatch engine
  Subscriber: VICI (driver app), subscribes to own driver_id only

  Payload:
  {
    "order_id": "550e8400-e29b-41d4-a716-446655440000",
    "pickup_address": "Jl. Sudirman No. 1, Jakarta Pusat",
    "dropoff_address": "Jl. Gatot Subroto No. 57, Jakarta Selatan",
    "fare_estimate": 24000,
    "pickup_lat": -6.1754,
    "pickup_lng": 106.8272,
    "dropoff_lat": -6.2415,
    "dropoff_lng": 106.7942,
    "expires_in": 15
  }
```

```
# Driver's accept/reject response
adird/offer/{driver_id}/response

  QoS:      1
  Retained: false
  Publisher: VICI (driver app)
  Subscriber: VIDI dispatch engine

  Payload:
  {
    "order_id": "550e8400-e29b-41d4-a716-446655440000",
    "accepted": true,
    "ts": 1710000000
  }
```

### 2.3 Trip Topics (VIDI → VICI Passenger)

```
# Driver GPS broadcast to passenger during active trip
adird/trip/{trip_id}/location

  QoS:      0 (high-frequency, loss-tolerant)
  Retained: false
  Publisher: VIDI (re-publishes from driver tracking)
  Subscriber: VICI (passenger app), subscribes on trip start, unsubscribes on completion

  Payload:
  {
    "lat": -6.1754,
    "lng": 106.8272,
    "speed": 35.2,
    "heading": 270
  }
```

```
# Trip lifecycle status updates
adird/trip/{trip_id}/status

  QoS:      1
  Retained: true (passenger gets current status on reconnect)
  Publisher: VIDI
  Subscriber: VICI (passenger app)

  Payload:
  {
    "status": "accepted" | "en_route" | "arrived" | "ongoing" | "completed",
    "driver_name": "Budi Santoso",
    "plate": "B 1234 XYZ",
    "eta_minutes": 8
  }
```

### 2.4 Zone / Surge Topics (VIDI → All VICI)

```
# Surge pricing update for a zone
adird/zone/{zone_id}/surge

  QoS:      0
  Retained: true (new driver connection gets current surge immediately)
  Publisher: VIDI pricing engine
  Subscriber: VICI (all drivers use wildcard adird/zone/#)

  Payload:
  {
    "zone_id": "jkt-sudirman",
    "multiplier": 1.5,
    "demand_level": "high"
  }
```

```
# Live demand heatmap cell — high-frequency, consumed by VINI
adird/zone/demand

  QoS:      0
  Retained: false
  Publisher: VIDI
  Subscriber: VINI dashboard

  Payload:
  {
    "zone_id": "jkt-sudirman",
    "active_orders": 12,
    "online_drivers": 5
  }
```

### 2.5 VINI Dashboard Topics (EMQX → VINI Web)

VINI subscribes using wildcards. It is a read-only subscriber on all operational topics.

```
# All driver GPS — live map rendering
adird/tracking/driver/+     QoS 0   wildcard single-level

# All zone surge overlays
adird/zone/+/surge          QoS 0   wildcard single-level

# System-wide operational alerts
adird/system/alerts         QoS 1
```

### Topic Summary Table

| Topic Pattern | Publisher | Subscriber | QoS | Retained |
|---|---|---|---|---|
| `adird/tracking/driver/{id}` | VICI driver | VIDI, VINI | 0 | No |
| `adird/driver/{id}/status` | VICI, EMQX LWT | VIDI | 1 | Yes |
| `adird/offer/{id}` | VIDI | VICI driver | 1 | No |
| `adird/offer/{id}/response` | VICI driver | VIDI | 1 | No |
| `adird/trip/{id}/location` | VIDI | VICI passenger | 0 | No |
| `adird/trip/{id}/status` | VIDI | VICI passenger | 1 | Yes |
| `adird/zone/{id}/surge` | VIDI | VICI drivers | 0 | Yes |
| `adird/zone/demand` | VIDI | VINI | 0 | No |
| `adird/system/alerts` | VIDI | VINI | 1 | No |

---

## 3. EMQX Configuration

### 3.1 Authentication — JWT (RS256)

EMQX validates JWTs directly using the RS256 public key. No round-trip to VIDI per connection.

```hocon
# /opt/emqx/etc/emqx.conf

authentication = [
  {
    mechanism = jwt
    from = password          # JWT is passed as the MQTT password field
    use_jwks = false
    algorithm = RS256
    public_key = "/opt/emqx/etc/jwt_public_key.pem"

    # Claims that must match exactly
    verify_claims {
      "iss" = "adird"
      "role" = "${username}"  # MQTT username must equal the role claim in JWT
    }
  }
]
```

JWT payload issued by VIDI at login:

```json
{
  "iss": "adird",
  "sub": "driver_abc123",
  "role": "driver",
  "device_id": "androidid789",
  "exp": 1710086400
}
```

The MQTT username field carries the role (`driver`, `passenger`, `vini`). The password field carries the signed JWT. EMQX verifies the signature and claim match.

### 3.2 Authorization — ACL Rules

```hocon
# /opt/emqx/etc/emqx.conf

authorization {
  no_match = deny           # deny-by-default
  deny_action = disconnect  # disconnect on ACL violation, not just reject

  sources = [
    {
      type = file
      path = "/opt/emqx/etc/acl.conf"
    }
  ]
}
```

```erlang
%% /opt/emqx/etc/acl.conf
%%
%% Variable substitution:
%%   ${clientid}  = MQTT clientId of the connecting client
%%   ${username}  = MQTT username field

%% Driver: publish own GPS, publish offer responses, subscribe to own offers and all zones
{allow, {user, "driver"}, publish,   ["adird/tracking/driver/${clientid}"]}.
{allow, {user, "driver"}, publish,   ["adird/offer/${clientid}/response"]}.
{allow, {user, "driver"}, publish,   ["adird/driver/${clientid}/status"]}.
{allow, {user, "driver"}, subscribe, ["adird/offer/${clientid}"]}.
{allow, {user, "driver"}, subscribe, ["adird/zone/#"]}.

%% Passenger: subscribe to trip updates (read-only during active trip)
{allow, {user, "passenger"}, subscribe, ["adird/trip/+/location"]}.
{allow, {user, "passenger"}, subscribe, ["adird/trip/+/status"]}.

%% VIDI backend service: full access (publishes offers, trip status, surge; subscribes to all)
{allow, {user, "vidi"}, all, ["#"]}.

%% VINI dashboard: subscribe-only to all topics
{allow, {user, "vini"}, subscribe, ["#"]}.

%% Default: deny everything else
{deny, all, all, ["#"]}.
```

Note: the driver ACL uses `${clientid}` for publish topics. This means a driver can only publish to the topic that matches their own clientId. Format: `driver_{user_id}_{device_id}`. The `driver_` prefix is stripped — see Section 8 for clientId format.

### 3.3 EMQX Rule Engine — Route GPS to VIDI

EMQX Rule Engine forwards GPS messages to VIDI via HTTP WebHook. This keeps the MQTT broker and business logic cleanly separated.

```sql
-- Rule: forward all driver GPS tracking messages to VIDI internal API
SELECT
  topic,
  clientid,
  payload,
  timestamp
FROM
  "adird/tracking/driver/+"
WHERE
  is_not_null(payload.lat) AND
  is_not_null(payload.lng) AND
  payload.lat >= -90 AND payload.lat <= 90 AND
  payload.lng >= -180 AND payload.lng <= 180
```

Action: HTTP WebHook POST to `http://vidi-internal:8080/internal/mqtt/tracking`

Headers: `Content-Type: application/json`, `X-Internal-Secret: {shared_secret}`

Alternative: EMQX Rule Engine writes directly to Redis via the Redis connector (see Section 6).

### 3.4 LWT Configuration — Client-Side (VICI)

The Last Will is registered by VICI at connection time. EMQX publishes it automatically when the TCP connection is lost without a clean DISCONNECT.

```kotlin
// VICI sets LWT in MqttConnectOptions before calling client.connect()
val connectOptions = MqttConnectOptions().apply {
    isCleanSession = false          // persistent session: broker queues QoS 1 messages during disconnect
    keepAliveInterval = 30          // PING/PONG every 30 seconds

    // Last Will: auto-published by broker if connection drops unexpectedly
    setWill(
        "adird/driver/$driverId/status",
        """{"status":"offline","ts":${System.currentTimeMillis() / 1000}}""".toByteArray(),
        1,    // QoS 1
        true  // retained: broker holds this as the last known status
    )
}
```

VIDI subscribes to `adird/driver/+/status` and marks the driver unavailable in Redis on receipt of an offline status payload from any source (clean logout or LWT).

---

## 4. VIDI Go Backend — MQTT Integration

### 4.1 MQTT Client Setup

```go
// internal/mqtt/client.go
package mqtt

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"
    "time"

    mqtt "github.com/eclipse/paho.mqtt.golang"
    "github.com/redis/go-redis/v9"
    "github.com/rs/zerolog/log"
)

type Client struct {
    client   mqtt.Client
    redis    *redis.Client
    dispatch DispatchHandler
}

type Config struct {
    EMQXHost      string
    VIDIServiceJWT string
}

type DispatchHandler interface {
    HandleOfferResponse(driverID, orderID string, accepted bool)
}

func NewClient(cfg Config, redisClient *redis.Client, dispatch DispatchHandler) (*Client, error) {
    mc := &Client{
        redis:    redisClient,
        dispatch: dispatch,
    }

    opts := mqtt.NewClientOptions().
        AddBroker(fmt.Sprintf("tcp://%s:1883", cfg.EMQXHost)).
        SetClientID("vidi_backend_server01").
        SetUsername("vidi").
        SetPassword(cfg.VIDIServiceJWT).
        SetCleanSession(false).
        SetAutoReconnect(true).
        SetMaxReconnectInterval(30 * time.Second).
        SetOnConnectHandler(func(c mqtt.Client) {
            log.Info().Msg("MQTT connected to EMQX")
            mc.resubscribe(c)
        }).
        SetConnectionLostHandler(func(c mqtt.Client, err error) {
            log.Error().Err(err).Msg("MQTT connection lost, auto-reconnect pending")
        })

    client := mqtt.NewClient(opts)
    token := client.Connect()
    token.Wait()
    if err := token.Error(); err != nil {
        return nil, fmt.Errorf("mqtt connect: %w", err)
    }

    mc.client = client
    return mc, nil
}

func (mc *Client) resubscribe(c mqtt.Client) {
    subs := map[string]byte{
        "adird/tracking/driver/+": 0, // QoS 0: GPS, high-volume
        "adird/offer/+/response":  1, // QoS 1: offer responses, must not lose
        "adird/driver/+/status":   1, // QoS 1: driver online/offline
    }
    for topic, qos := range subs {
        c.Subscribe(topic, qos, mc.dispatch_message)
    }
}

func (mc *Client) dispatch_message(c mqtt.Client, msg mqtt.Message) {
    parts := strings.Split(msg.Topic(), "/")
    switch {
    case len(parts) == 4 && parts[1] == "tracking" && parts[2] == "driver":
        mc.onDriverTracking(parts[3], msg.Payload())
    case len(parts) == 4 && parts[1] == "offer" && parts[3] == "response":
        mc.onOfferResponse(parts[2], msg.Payload())
    case len(parts) == 4 && parts[1] == "driver" && parts[3] == "status":
        mc.onDriverStatus(parts[2], msg.Payload())
    }
}
```

### 4.2 GPS Tracking Handler

```go
// internal/mqtt/tracking.go

type gpsPayload struct {
    Lat     float64 `json:"lat"`
    Lng     float64 `json:"lng"`
    Speed   float64 `json:"speed"`
    Heading int     `json:"heading"`
    Ts      int64   `json:"ts"`
}

func (mc *Client) onDriverTracking(driverID string, raw []byte) {
    var payload gpsPayload
    if err := json.Unmarshal(raw, &payload); err != nil {
        return
    }

    if !isValidGPS(payload.Lat, payload.Lng, payload.Speed) {
        return
    }

    ctx := context.Background()

    // Update Redis GEO index for nearby-driver queries
    mc.redis.GeoAdd(ctx, "drivers:online", &redis.GeoLocation{
        Name:      driverID,
        Longitude: payload.Lng,
        Latitude:  payload.Lat,
    })

    // Update driver state hash (expires after 60s of no update = stale)
    mc.redis.HSet(ctx, "driver:"+driverID,
        "lat", payload.Lat,
        "lng", payload.Lng,
        "speed", payload.Speed,
        "heading", payload.Heading,
        "updated_at", payload.Ts,
    )
    mc.redis.Expire(ctx, "driver:"+driverID, 60*time.Second)

    // If driver is on an active trip, forward location to the passenger
    tripID, err := mc.redis.HGet(ctx, "driver:"+driverID, "active_trip_id").Result()
    if err == nil && tripID != "" {
        mc.PublishTripLocation(tripID, payload.Lat, payload.Lng, payload.Speed, payload.Heading)
    }
}

func isValidGPS(lat, lng, speed float64) bool {
    if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
        return false
    }
    if speed < 0 || speed > 150 { // >150 km/h is implausible for ojek
        return false
    }
    return true
}
```

### 4.3 Publish Offer to Driver

```go
// internal/mqtt/offers.go

type OfferPayload struct {
    OrderID        string  `json:"order_id"`
    PickupAddress  string  `json:"pickup_address"`
    DropoffAddress string  `json:"dropoff_address"`
    FareEstimate   int64   `json:"fare_estimate"`
    PickupLat      float64 `json:"pickup_lat"`
    PickupLng      float64 `json:"pickup_lng"`
    DropoffLat     float64 `json:"dropoff_lat"`
    DropoffLng     float64 `json:"dropoff_lng"`
    ExpiresIn      int     `json:"expires_in"`
}

func (mc *Client) SendOffer(ctx context.Context, driverID string, order Order) error {
    topic := fmt.Sprintf("adird/offer/%s", driverID)

    payload := OfferPayload{
        OrderID:        order.ID,
        PickupAddress:  order.PickupAddress,
        DropoffAddress: order.DropoffAddress,
        FareEstimate:   order.FareEstimate,
        PickupLat:      order.PickupLat,
        PickupLng:      order.PickupLng,
        DropoffLat:     order.DropoffLat,
        DropoffLng:     order.DropoffLng,
        ExpiresIn:      15,
    }

    data, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("marshal offer: %w", err)
    }

    // QoS 1: broker retransmits until driver ACKs
    token := mc.client.Publish(topic, 1, false, data)
    if !token.WaitTimeout(5 * time.Second) {
        return fmt.Errorf("mqtt publish offer: timeout")
    }
    return token.Error()
}
```

### 4.4 Offer Response Handler

```go
func (mc *Client) onOfferResponse(driverID string, raw []byte) {
    var response struct {
        OrderID  string `json:"order_id"`
        Accepted bool   `json:"accepted"`
        Ts       int64  `json:"ts"`
    }
    if err := json.Unmarshal(raw, &response); err != nil {
        return
    }

    // Route to the waiting channel in the dispatch engine
    mc.dispatch.HandleOfferResponse(driverID, response.OrderID, response.Accepted)
}
```

### 4.5 Publish Trip Status and Location

```go
// internal/mqtt/trips.go

type TripStatus struct {
    Status     string `json:"status"`
    DriverName string `json:"driver_name"`
    Plate      string `json:"plate"`
    ETAMinutes int    `json:"eta_minutes,omitempty"`
}

func (mc *Client) PublishTripStatus(tripID string, status TripStatus) error {
    topic := fmt.Sprintf("adird/trip/%s/status", tripID)
    data, _ := json.Marshal(status)
    // QoS 1, retained: passenger reconnecting gets current status immediately
    token := mc.client.Publish(topic, 1, true, data)
    token.WaitTimeout(5 * time.Second)
    return token.Error()
}

func (mc *Client) PublishTripLocation(tripID string, lat, lng, speed float64, heading int) {
    topic := fmt.Sprintf("adird/trip/%s/location", tripID)
    data, _ := json.Marshal(map[string]interface{}{
        "lat": lat, "lng": lng, "speed": speed, "heading": heading,
    })
    // QoS 0: high-frequency location, loss-tolerant
    mc.client.Publish(topic, 0, false, data)
}
```

### 4.6 Driver Status Handler

```go
func (mc *Client) onDriverStatus(driverID string, raw []byte) {
    var status struct {
        Status string `json:"status"`
        Ts     int64  `json:"ts"`
    }
    if err := json.Unmarshal(raw, &status); err != nil {
        return
    }

    ctx := context.Background()
    if status.Status == "offline" {
        // Remove from GEO index (no longer dispatchable)
        mc.redis.ZRem(ctx, "drivers:online", driverID)
        mc.redis.HSet(ctx, "driver:"+driverID, "status", "offline")
    } else {
        mc.redis.HSet(ctx, "driver:"+driverID, "status", "online")
    }
}
```

---

## 5. VICI Android — MQTT Client (Eclipse Paho)

### 5.1 Dependencies

```kotlin
// build.gradle.kts (app module)
dependencies {
    // Eclipse Paho MQTT Android client
    implementation("org.eclipse.paho:org.eclipse.paho.client.mqttv3:1.2.5")
    implementation("org.eclipse.paho:org.eclipse.paho.android.service:1.1.1")

    // Alternative (more modern async API, consider for future):
    // implementation("com.hivemq:hivemq-mqtt-client:1.3.3")
}
```

### 5.2 Driver MQTT Client

```kotlin
// app/src/main/java/id/adird/vici/mqtt/VICIMQTTClient.kt
package id.adird.vici.mqtt

import android.content.Context
import android.provider.Settings
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import org.eclipse.paho.android.service.MqttAndroidClient
import org.eclipse.paho.client.mqttv3.*
import javax.net.ssl.SSLContext

class VICIDriverMQTTClient(
    private val context: Context,
    private val driverId: String,
    private val jwtToken: String,
    private val onOfferReceived: (OfferPayload) -> Unit,
    private val onSurgeUpdated: (SurgePayload) -> Unit
) {
    private lateinit var client: MqttAndroidClient

    fun connect(onConnected: () -> Unit, onFailed: (Throwable) -> Unit) {
        val clientId = "driver_${driverId}_${deviceId()}"

        client = MqttAndroidClient(context, BROKER_URL, clientId)

        val options = MqttConnectOptions().apply {
            isCleanSession = false          // persistent session: broker queues offers during disconnect
            keepAliveInterval = 30          // PING every 30s — balance between keepalive and battery
            userName = "driver"
            password = jwtToken.toCharArray()

            // Last Will: published by broker if TCP drops without clean DISCONNECT
            setWill(
                "adird/driver/$driverId/status",
                """{"status":"offline","ts":${System.currentTimeMillis() / 1000}}""".toByteArray(),
                1,    // QoS 1
                true  // retained
            )

            socketFactory = SSLContext.getDefault().socketFactory
        }

        client.setCallback(object : MqttCallbackExtended {
            override fun connectComplete(reconnect: Boolean, serverURI: String) {
                subscribeToOffers()
                subscribeToZoneSurge()
                publishOnlineStatus()
                onConnected()
            }

            override fun messageArrived(topic: String, message: MqttMessage) {
                handleMessage(topic, message)
            }

            override fun connectionLost(cause: Throwable?) {
                // MqttAndroidClient handles reconnect automatically using persistent session
            }

            override fun deliveryComplete(token: IMqttDeliveryToken) {}
        })

        client.connect(options, null, object : IMqttActionListener {
            override fun onSuccess(asyncActionToken: IMqttToken) {}
            override fun onFailure(token: IMqttToken, exception: Throwable) {
                onFailed(exception)
            }
        })
    }

    // QoS 0: fire-and-forget, called from LocationManager callback
    fun publishLocation(lat: Double, lng: Double, speed: Float, heading: Int) {
        if (!client.isConnected) return

        val payload = """{"lat":$lat,"lng":$lng,"speed":$speed,"heading":$heading,"ts":${System.currentTimeMillis() / 1000}}"""
        client.publish(
            "adird/tracking/driver/$driverId",
            payload.toByteArray(),
            0,    // QoS 0
            false // not retained
        )
    }

    // QoS 1: broker retransmits until this client ACKs
    fun respondToOffer(orderId: String, accepted: Boolean) {
        val payload = """{"order_id":"$orderId","accepted":$accepted,"ts":${System.currentTimeMillis() / 1000}}"""
        client.publish(
            "adird/offer/$driverId/response",
            payload.toByteArray(),
            1,
            false
        )
    }

    fun publishOnlineStatus() {
        val payload = """{"status":"online","ts":${System.currentTimeMillis() / 1000}}"""
        client.publish("adird/driver/$driverId/status", payload.toByteArray(), 1, true)
    }

    fun publishOfflineStatus() {
        val payload = """{"status":"offline","ts":${System.currentTimeMillis() / 1000}}"""
        client.publish("adird/driver/$driverId/status", payload.toByteArray(), 1, true)
    }

    fun disconnect() {
        publishOfflineStatus()
        client.disconnect()
    }

    private fun subscribeToOffers() {
        // QoS 1: guaranteed delivery; broker queues offer if driver temporarily disconnected
        client.subscribe("adird/offer/$driverId", 1)
    }

    private fun subscribeToZoneSurge() {
        // QoS 0: surge updates are retained; driver gets current multiplier on reconnect regardless
        client.subscribe("adird/zone/#", 0)
    }

    private fun handleMessage(topic: String, message: MqttMessage) {
        val parts = topic.split("/")
        when {
            parts.size == 3 && parts[0] == "adird" && parts[1] == "offer" -> {
                val offer = Json.decodeFromString<OfferPayload>(String(message.payload))
                onOfferReceived(offer)
            }
            parts.size == 4 && parts[1] == "zone" && parts[3] == "surge" -> {
                val surge = Json.decodeFromString<SurgePayload>(String(message.payload))
                onSurgeUpdated(surge)
            }
        }
    }

    private fun deviceId(): String =
        Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID)

    companion object {
        private const val BROKER_URL = "ssl://mqtt.adird.id:8883"
    }
}
```

### 5.3 Passenger MQTT Client

Passengers are subscribe-only during an active trip. They do not publish any MQTT messages.

```kotlin
// app/src/main/java/id/adird/vici/mqtt/VICIPassengerMQTTClient.kt
package id.adird.vici.mqtt

import android.content.Context
import android.provider.Settings
import kotlinx.serialization.json.Json
import org.eclipse.paho.android.service.MqttAndroidClient
import org.eclipse.paho.client.mqttv3.*

class VICIPassengerMQTTClient(
    private val context: Context,
    private val passengerId: String,
    private val jwtToken: String
) {
    private lateinit var client: MqttAndroidClient

    fun connect(onConnected: () -> Unit) {
        val clientId = "passenger_${passengerId}_${deviceId()}"
        client = MqttAndroidClient(context, BROKER_URL, clientId)

        val options = MqttConnectOptions().apply {
            isCleanSession = false
            keepAliveInterval = 60  // passengers are less latency-sensitive than drivers
            userName = "passenger"
            password = jwtToken.toCharArray()
        }

        client.connect(options, null, object : IMqttActionListener {
            override fun onSuccess(token: IMqttToken) { onConnected() }
            override fun onFailure(token: IMqttToken, exception: Throwable) {}
        })
    }

    fun subscribeTripUpdates(
        tripId: String,
        onLocation: (lat: Double, lng: Double, speed: Float, heading: Int) -> Unit,
        onStatus: (status: String, driverName: String, plate: String, etaMinutes: Int) -> Unit
    ) {
        // QoS 0: high-frequency location updates, loss-tolerant
        client.subscribe("adird/trip/$tripId/location", 0) { _, msg ->
            val loc = Json.decodeFromString<LocationPayload>(String(msg.payload))
            onLocation(loc.lat, loc.lng, loc.speed, loc.heading)
        }

        // QoS 1: trip status is critical (accepted, arrived, completed)
        // Retained: passenger gets current status even if they connect mid-trip
        client.subscribe("adird/trip/$tripId/status", 1) { _, msg ->
            val status = Json.decodeFromString<TripStatusPayload>(String(msg.payload))
            onStatus(status.status, status.driverName, status.plate, status.etaMinutes)
        }
    }

    fun unsubscribeTripUpdates(tripId: String) {
        client.unsubscribe(arrayOf(
            "adird/trip/$tripId/location",
            "adird/trip/$tripId/status"
        ))
    }

    private fun deviceId(): String =
        Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID)

    companion object {
        private const val BROKER_URL = "ssl://mqtt.adird.id:8883"
    }
}
```

---

## 6. EMQX Rule Engine — GPS to Redis Pipeline

An alternative to VIDI subscribing to all GPS topics is using the EMQX Rule Engine with a Redis connector to write location data directly to Redis. This offloads GPS ingestion from VIDI entirely. VIDI only handles business logic.

```sql
-- EMQX Rule: GPS tracking → Redis GEO + Hash directly
-- Rule name: gps_to_redis

SELECT
  regex_extract(topic, 'adird/tracking/driver/(.+)', 1) AS driver_id,
  payload.lat  AS lat,
  payload.lng  AS lng,
  payload.speed AS speed,
  payload.heading AS heading,
  timestamp    AS ts
FROM
  "adird/tracking/driver/+"
WHERE
  is_not_null(payload.lat) AND
  is_not_null(payload.lng) AND
  payload.lat >= -90  AND payload.lat  <= 90 AND
  payload.lng >= -180 AND payload.lng <= 180
```

Actions on this rule:
1. Redis `GEOADD drivers:online ${lng} ${lat} ${driver_id}`
2. Redis `HSET driver:${driver_id} lat ${lat} lng ${lng} speed ${speed} heading ${heading} updated_at ${ts}`
3. Redis `EXPIRE driver:${driver_id} 60`

When this rule engine pipeline is active, VIDI's `onDriverTracking` handler only needs to handle the trip-forwarding logic (re-publish to `adird/trip/{trip_id}/location`), not Redis writes.

---

## 7. Dispatch Engine — MQTT-Aware Offer Flow

The dispatch engine replaces WebSocket channel waiting with MQTT publish + in-memory channel waiting.

```go
// internal/dispatch/engine.go

type Engine struct {
    mqtt          *mqttclient.Client
    redis         *redis.Client
    offerMu       sync.RWMutex
    offerChannels map[string]chan bool // key: "{driverID}:{orderID}"
}

// OfferRide sends an offer to a driver and waits for accept/reject.
// Called by the dispatch loop for each candidate driver.
func (e *Engine) OfferRide(ctx context.Context, driverID string, order Order) (bool, error) {
    lockKey := fmt.Sprintf("dispatch:lock:%s", driverID)

    // Prevent double-offering: atomic lock with 20s TTL
    set, err := e.redis.SetNX(ctx, lockKey, order.ID, 20*time.Second).Result()
    if err != nil {
        return false, fmt.Errorf("redis lock: %w", err)
    }
    if !set {
        return false, ErrDriverBusy
    }

    // Register in-memory channel before publishing offer
    // (avoids race: response arrives before channel is ready)
    respChan := make(chan bool, 1)
    chanKey := driverID + ":" + order.ID
    e.offerMu.Lock()
    e.offerChannels[chanKey] = respChan
    e.offerMu.Unlock()

    defer func() {
        e.redis.Del(ctx, lockKey)
        e.offerMu.Lock()
        delete(e.offerChannels, chanKey)
        e.offerMu.Unlock()
    }()

    // Publish offer via MQTT QoS 1
    if err := e.mqtt.SendOffer(ctx, driverID, order); err != nil {
        return false, fmt.Errorf("mqtt send offer: %w", err)
    }

    // Wait for driver response via MQTT subscription
    select {
    case accepted := <-respChan:
        return accepted, nil
    case <-time.After(15 * time.Second):
        return false, ErrOfferTimeout
    case <-ctx.Done():
        return false, ctx.Err()
    }
}

// HandleOfferResponse is called by the MQTT client's onOfferResponse handler.
// It routes the driver's accept/reject to the waiting OfferRide goroutine.
func (e *Engine) HandleOfferResponse(driverID, orderID string, accepted bool) {
    chanKey := driverID + ":" + orderID
    e.offerMu.RLock()
    ch, exists := e.offerChannels[chanKey]
    e.offerMu.RUnlock()

    if exists {
        select {
        case ch <- accepted:
        default:
            // Channel already consumed or timed out; discard
        }
    }
}
```

---

## 8. Single Device Enforcement

One MQTT connection per account at any time. Prevents session hijacking and duplicate GPS streams.

### ClientId Format

```
{role}_{userId}_{deviceId}

Examples:
  driver_abc123uuid_androidid789abc
  passenger_xyz456uuid_androidid012def
  vidi_backend_server01
  vini_admin_browsersession123
```

### EMQX Kick-Old-Session

```hocon
# emqx.conf
mqtt {
  # When a new connection arrives with an existing clientId,
  # kick the old session and accept the new one.
  allow_override = true
}
```

### Device ID Validation in VIDI

VIDI embeds `device_id` in the issued JWT:

```go
// internal/auth/token.go

type Claims struct {
    jwt.RegisteredClaims
    Role     string `json:"role"`
    DeviceID string `json:"device_id"`
}

// At login: store device_id in JWT and in Redis
func (a *Auth) IssueToken(userID, role, deviceID string) (string, error) {
    claims := Claims{
        RegisteredClaims: jwt.RegisteredClaims{
            Issuer:    "adird",
            Subject:   userID,
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
        },
        Role:     role,
        DeviceID: deviceID,
    }
    // ...sign and return token
}
```

EMQX ACL rule enforces that the clientId matches the pattern `{role}_{userId}_{deviceId}` where `deviceId` matches the JWT claim. If a driver attempts to connect with a spoofed clientId, the ACL rejects the publish to `adird/tracking/driver/{clientid}` because `${clientid}` would not match the `driver_id` they are trying to impersonate.
