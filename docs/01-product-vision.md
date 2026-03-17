---
title: Product Vision
tags: [vision, strategy, business]
created: 2026-03-16
---

# Product Vision

> **See also**: [[00-index]] | [[12-differentiation]] | [[13-roadmap]]

---

## The Problem

Gojek and Grab extract **20–30% commission** from every driver trip.

A driver completing 10 trips/day at Rp 30,000 average fare:
- Pays **Rp 60,000–90,000** to the platform **daily**
- Loses **Rp 1.8–2.7 million/month** to platform fees

These platforms justified this during their growth phase — they funded driver acquisition, subsidized fares, and built brand awareness. **That phase is over.** Drivers are locked in by habit and passenger network effects, not by value delivered.

---

## The Opportunity

A platform charging **10–12% commission** creates an immediate wage increase:

| Scenario | Daily Trips | Avg Fare | Grab (25%) | ADIRD (10%) | Monthly Gain |
|----------|-------------|----------|------------|-------------|--------------|
| Motor driver | 15 | Rp 25,000 | Rp 281,250 | Rp 337,500 | **+Rp 1.7M** |
| Car driver | 8 | Rp 55,000 | Rp 330,000 | Rp 396,000 | **+Rp 2.0M** |

This message **spreads organically through driver WhatsApp groups** without any marketing spend.

---

## Go-To-Market Strategy

### Niche First
Start in **one zone** (Menteng / Sudirman corridor), own it completely, then expand.

Gojek cannot respond with a special commission deal for 30 drivers in Menteng without triggering a platform-wide policy change. **You can move faster than their bureaucracy.**

### Growth Funnel
```
Driver recruitment (WhatsApp communities)
    ↓
Driver network grows in one zone
    ↓
Passenger word-of-mouth ("the app with honest pricing")
    ↓
Zone density increases → better dispatch times
    ↓
Expand to adjacent zones
```

---

## Why This Stack Wins on Cost

| Cost Item | Gojek/Grab | ADIRD |
|-----------|-----------|-------|
| Maps | Google Maps API (~$5/1000 loads) | OpenStreetMap + OpenFreeMap = **$0** |
| Routing | Google Directions API | OSRM self-hosted = **$0** |
| Infrastructure | AWS/GCP enterprise | Hetzner Cloud = **~$20/mo** |
| Team | 100s of engineers | 1 engineer |
| **Monthly total** | **$10,000s** | **~$25/mo** |

### The OSM Long-Term Advantage

The OSM advantage is underrated. You **own** the routing data.

You can tune the motorcycle profile for Jakarta's alleys (*gang*, *jalan tikus*) in ways Google will never prioritize. Over 18 months, **your routing will be more accurate for ojek than any commercial provider**.

---

## How to Compete Against Gojek/Grab

| Dimension | Gojek/Grab | ADIRD Advantage |
|-----------|-----------|-----------------|
| Commission | 20–25% | 10% — direct driver loyalty |
| Routing | Google Maps (car-centric) | Custom ojek motorcycle profile |
| Transparency | Black-box pricing | Full fare breakdown, show driver earnings |
| Trust | Corporate | Community-first, cooperative model |
| Map cost | $$$$ per API call | $0 — open source |
| Traffic data | HERE/Google | Crowdsourced from own drivers |

---

## Vision Statement

> Build Jakarta's first **driver-first ride platform** — lower commission, honest pricing, motorcycle-optimized routing. Owned by a small team, not a corporation.

---

*See [[12-differentiation]] for innovation ideas that widen this competitive moat.*
