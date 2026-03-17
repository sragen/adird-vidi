---
title: Real-Time Driver Tracking
tags: [tracking, websocket, redis, kotlin, gps]
created: 2026-03-16
---

# Real-Time Driver Tracking

> **See also**: [[02-system-architecture]] | [[04-dispatch-algorithm]] | [[08-database-design]]

---

## Architecture Overview

```
VICI Driver App (Kotlin)           EMQX Broker              VIDI Go Backend
    │                                   │                         │
    │  ForegroundService                │                         │
    │  FusedLocationProvider            │                         │
    │                                   │                         │
    │─── MQTT QoS 0 ──────────────────► │                         │
    │  adird/tracking/driver/{id}       │                         │
    │  {lat,lng,speed,heading,ts}       │                         │
    │                                   │─── Rule Engine ────────►│
    │                                   │    WebHook POST         │
    │                                   │    /internal/tracking   │
    │                                   │                         │─── GEOADD Redis
    │                                   │                         │─── HSET Redis
    │                                   │                         │
    │                                   │◄── MQTT QoS 1 ──────────│
    │◄── MQTT QoS 1 ────────────────────│  adird/offer/{driver_id}│
    │  adird/offer/{driver_id}          │                         │
    │  {order_id, fare, pickup, ...}    │                         │
    │                                   │                         │
    │─── MQTT QoS 1 ──────────────────► │                         │
    │  adird/offer/{id}/response        │──────────────────────── ►│
    │  {order_id, accepted: true}       │                         │─── assignDriver()
    │                                   │                         │
    │                                   │◄── MQTT QoS 0 ──────────│
VICI Passenger App                      │  adird/trip/{id}/location│
    │                                   │                         │
    │─── subscribe ───────────────────► │                         │
    │  adird/trip/{trip_id}/location    │                         │
    │◄── driver location updates ───────│                         │
```

> **No more WebSocket hub in VIDI.** EMQX handles all persistent connections. VIDI is a pure MQTT client + REST API server.

---

## GPS Update Frequency

| Driver State | Update Interval | Min Distance |
|-------------|----------------|--------------|
| Moving (speed > 5km/h) | **4 seconds** | 5 meters |
| Idle (speed < 5km/h) | **15 seconds** | — |
| App in background | Same (Foreground Service) | — |

**Battery vs accuracy tradeoff**: 4 seconds is sufficient for smooth map animation on passenger side (interpolate between updates). Reduces from continuous GPS polling which drains battery significantly.

---

## Android: Foreground Service

Android 8+ requires a Foreground Service with persistent notification for background GPS access. Without this, GPS stops when the app goes to background.

### AndroidManifest.xml

```xml
<manifest>
    <!-- Required permissions -->
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE" />
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_LOCATION" />
    <uses-permission android:name="android.permission.ACCESS_FINE_LOCATION" />
    <uses-permission android:name="android.permission.ACCESS_BACKGROUND_LOCATION" />

    <application>
        <service
            android:name=".service.LocationForegroundService"
            android:foregroundServiceType="location"
            android:exported="false" />
    </application>
</manifest>
```

### LocationForegroundService.kt

```kotlin
class LocationForegroundService : Service() {

    private lateinit var fusedLocationClient: FusedLocationProviderClient
    private lateinit var wsClient: DriverWebSocketClient
    private val locationBuffer = ArrayDeque<LocationUpdate>()
    private var currentSpeed = 0f

    private val locationCallback = object : LocationCallback() {
        override fun onLocationResult(result: LocationResult) {
            result.lastLocation?.let { location ->
                currentSpeed = location.speed

                val update = LocationUpdate(
                    lat = location.latitude,
                    lng = location.longitude,
                    speed = location.speed,
                    heading = location.bearing.toInt(),
                    timestamp = System.currentTimeMillis()
                )

                if (wsClient.isConnected()) {
                    // Flush buffered updates from disconnect period
                    while (locationBuffer.isNotEmpty()) {
                        wsClient.sendLocation(locationBuffer.removeFirst())
                    }
                    wsClient.sendLocation(update)
                } else {
                    locationBuffer.addLast(update)
                    if (locationBuffer.size > 20) locationBuffer.removeFirst() // cap
                }
            }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(NOTIF_ID, buildNotification())
        startLocationUpdates()
        return START_STICKY // restart if killed by system
    }

    private fun startLocationUpdates() {
        // Adaptive interval: 4s moving, 15s idle
        val interval = if (currentSpeed > 1.4f) 4_000L else 15_000L

        val request = LocationRequest.Builder(
            Priority.PRIORITY_HIGH_ACCURACY, interval
        ).setMinUpdateDistanceMeters(5f).build()

        fusedLocationClient.requestLocationUpdates(
            request, locationCallback, Looper.getMainLooper()
        )
    }

    private fun buildNotification(): Notification {
        val channel = NotificationChannel(
            "location_channel", "Driver Tracking",
            NotificationManager.IMPORTANCE_LOW // low: no sound
        )
        getSystemService(NotificationManager::class.java)
            .createNotificationChannel(channel)

        return NotificationCompat.Builder(this, "location_channel")
            .setContentTitle("ADIRD - Kamu Online")
            .setContentText("Menunggu penumpang...")
            .setSmallIcon(R.drawable.ic_car)
            .setOngoing(true) // cannot be dismissed
            .build()
    }

    override fun onBind(intent: Intent?): IBinder? = null
}
```

---

## Android: WebSocket Client (OkHttp)

```kotlin
class DriverWebSocketClient(
    private val token: String,
    private val onConnectionChange: (Boolean) -> Unit
) : WebSocketListener() {

    private var webSocket: WebSocket? = null
    private var reconnectDelay = 1_000L
    private val client = OkHttpClient.Builder()
        .pingInterval(20, TimeUnit.SECONDS) // keep-alive ping
        .build()

    fun connect() {
        val request = Request.Builder()
            .url("wss://api.adird.id/ws/driver")
            .addHeader("Authorization", "Bearer $token")
            .build()
        webSocket = client.newWebSocket(request, this)
    }

    fun disconnect() {
        webSocket?.close(1000, "Driver went offline")
        webSocket = null
    }

    fun isConnected(): Boolean = webSocket != null

    override fun onOpen(ws: WebSocket, response: Response) {
        reconnectDelay = 1_000L // reset backoff on success
        onConnectionChange(true)
    }

    override fun onFailure(ws: WebSocket, t: Throwable, response: Response?) {
        webSocket = null
        onConnectionChange(false)
        scheduleReconnect()
    }

    override fun onClosed(ws: WebSocket, code: Int, reason: String) {
        webSocket = null
        onConnectionChange(false)
        if (code != 1000) scheduleReconnect() // don't reconnect on intentional close
    }

    fun sendLocation(update: LocationUpdate) {
        val json = Json.encodeToString(update)
        webSocket?.send(json)
    }

    private fun scheduleReconnect() {
        Handler(Looper.getMainLooper()).postDelayed({
            reconnectDelay = minOf(reconnectDelay * 2, 30_000L) // max 30s
            connect()
        }, reconnectDelay)
    }
}

@Serializable
data class LocationUpdate(
    val lat: Double,
    val lng: Double,
    val speed: Float,
    val heading: Int,
    val timestamp: Long
)
```

---

## Go: WebSocket Hub

```go
type Hub struct {
    drivers    map[string]*DriverConn
    passengers map[string]*PassengerConn // tripID → conn
    mu         sync.RWMutex
    register   chan *DriverConn
    unregister chan *DriverConn
    broadcast  chan LocationBroadcast
    redis      *redis.Client
}

type LocationBroadcast struct {
    DriverID  string
    Lat, Lng  float64
    Speed     float64
    Heading   int
    Timestamp int64
}

func (h *Hub) Run(ctx context.Context) {
    for {
        select {
        case conn := <-h.register:
            h.mu.Lock()
            h.drivers[conn.DriverID] = conn
            h.mu.Unlock()

        case conn := <-h.unregister:
            h.mu.Lock()
            delete(h.drivers, conn.DriverID)
            h.mu.Unlock()
            // Immediately remove from GEO set on clean disconnect
            h.redis.ZRem(ctx, "drivers:online", conn.DriverID)

        case msg := <-h.broadcast:
            h.processLocationUpdate(ctx, msg)

        case <-ctx.Done():
            return
        }
    }
}

func (h *Hub) processLocationUpdate(ctx context.Context, msg LocationBroadcast) {
    // 1. Update Redis GEO index (for dispatch nearest-driver search)
    h.redis.GeoAdd(ctx, "drivers:online", &redis.GeoLocation{
        Name:      msg.DriverID,
        Longitude: msg.Lng,
        Latitude:  msg.Lat,
    })

    // 2. Update driver metadata hash (60s TTL = auto-expire if disconnected)
    h.redis.HSet(ctx, "driver:"+msg.DriverID,
        "lat", msg.Lat,
        "lng", msg.Lng,
        "speed", msg.Speed,
        "heading", msg.Heading,
        "updated_at", msg.Timestamp,
    )
    h.redis.Expire(ctx, "driver:"+msg.DriverID, 60*time.Second)

    // 3. Publish to passenger subscriber (if passenger is watching this driver)
    payload, _ := json.Marshal(msg)
    h.redis.Publish(ctx, "channel:driver:"+msg.DriverID, payload)
}
```

### Driver WebSocket Handler

```go
func (h *Hub) HandleDriverWS(w http.ResponseWriter, r *http.Request) {
    driverID := r.Context().Value(contextKeyDriverID).(string)

    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    defer conn.Close()

    driverConn := &DriverConn{
        DriverID: driverID,
        Conn:     conn,
    }
    h.register <- driverConn
    defer func() { h.unregister <- driverConn }()

    for {
        var msg LocationUpdate
        if err := conn.ReadJSON(&msg); err != nil {
            break // connection closed or error
        }

        // Apply map matching to reduce GPS drift
        snapped := h.mapMatcher.SnapToRoad(msg.Lat, msg.Lng)

        h.broadcast <- LocationBroadcast{
            DriverID:  driverID,
            Lat:       snapped.Lat,
            Lng:       snapped.Lng,
            Speed:     msg.Speed,
            Heading:   msg.Heading,
            Timestamp: msg.Timestamp,
        }
    }
}
```

---

## Stale Driver Cleanup

Background goroutine removes drivers from GEO set if their Redis hash has expired (heartbeat timeout):

```go
func (h *Hub) CleanupStaleDrivers(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    for {
        select {
        case <-ticker.C:
            members, _ := h.redis.ZRange(ctx, "drivers:online", 0, -1).Result()
            for _, driverID := range members {
                exists, _ := h.redis.Exists(ctx, "driver:"+driverID).Result()
                if exists == 0 {
                    // Hash expired (no heartbeat for 60s) → remove from GEO
                    h.redis.ZRem(ctx, "drivers:online", driverID)
                }
            }
        case <-ctx.Done():
            return
        }
    }
}
```

---

## Passenger Subscribes to Driver Location

```go
func (h *Hub) HandlePassengerWS(w http.ResponseWriter, r *http.Request) {
    tripID := chi.URLParam(r, "tripId")
    trip := h.orderRepo.GetTrip(r.Context(), tripID)

    conn, _ := upgrader.Upgrade(w, r, nil)
    defer conn.Close()

    // Subscribe to driver's Redis pub/sub channel
    pubsub := h.redis.Subscribe(r.Context(), "channel:driver:"+trip.DriverID)
    defer pubsub.Close()

    for {
        select {
        case msg := <-pubsub.Channel():
            // Forward driver location to passenger WebSocket
            conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))

        case <-r.Context().Done():
            return
        }
    }
}
```

---

## Redis Key Structure Summary

```bash
# Geospatial index of all online drivers
# Auto-cleaned by CleanupStaleDrivers goroutine
GEOADD drivers:online <lng> <lat> <driver_id>

# Driver metadata (60s TTL = auto offline if no heartbeat)
HSET driver:<id> status online lat -6.17 lng 106.82 speed 35 heading 270
EXPIRE driver:<id> 60

# Active trip hot state (1h TTL)
SET trip:<id> '{"status":"ongoing","driver_id":"...","passenger_id":"..."}' EX 3600

# Dispatch offer lock (20s TTL = offer window + buffer)
SET dispatch:lock:<driver_id> <order_id> NX EX 20

# Surge demand counter per zone (5min TTL)
INCR zone:demand:<zone_id>
EXPIRE zone:demand:<zone_id> 300

# Driver pub/sub channel (passenger subscribes)
SUBSCRIBE channel:driver:<driver_id>
PUBLISH channel:driver:<driver_id> <location_json>
```

---

## Performance Characteristics at 100 Drivers

| Operation | Expected Latency | Notes |
|-----------|-----------------|-------|
| GEOADD (location update) | < 1ms | Redis O(log N) |
| GEOSEARCH (nearest driver) | < 2ms | 100 drivers, 5km radius |
| WebSocket broadcast | < 5ms | goroutine per connection |
| Pub/sub delivery | < 10ms | Redis to passenger WS |
| Cleanup goroutine | 10s cycle | Non-blocking |

100 concurrent WebSocket connections and their goroutines consume ~50MB RAM total. Well within CX32 limits.

---

*See [[04-dispatch-algorithm]] for how GEO data drives dispatch, [[08-database-design]] for persistent trip location storage.*
