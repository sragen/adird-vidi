#!/bin/bash
# scripts/osrm-setup.sh
# Downloads and builds Jakarta OSM routing data for OSRM
# Run once: make osrm-build
# Time: ~5-10 minutes depending on internet speed

set -e

DATA_DIR="$(dirname "$0")/osrm-data"
PROFILE_DIR="$(dirname "$0")/osrm-profiles"
INDONESIA_URL="https://download.geofabrik.de/asia/indonesia-latest.osm.pbf"
JAKARTA_BBOX="106.5,-6.5,107.1,-6.0"

mkdir -p "$DATA_DIR"

echo "📦 Step 1/5: Checking osmium-tool..."
if ! command -v osmium &>/dev/null; then
  echo "Installing osmium-tool..."
  brew install osmium-tool
fi

echo "🌐 Step 2/5: Downloading Indonesia OSM (~800MB)..."
if [ ! -f "$DATA_DIR/indonesia.osm.pbf" ]; then
  curl -L --progress-bar "$INDONESIA_URL" -o "$DATA_DIR/indonesia.osm.pbf"
else
  echo "  → indonesia.osm.pbf already exists, skipping download"
fi

echo "✂️  Step 3/5: Extracting Jakarta region (bbox: $JAKARTA_BBOX)..."
if [ ! -f "$DATA_DIR/jakarta.osm.pbf" ]; then
  osmium extract \
    --bbox "$JAKARTA_BBOX" \
    "$DATA_DIR/indonesia.osm.pbf" \
    -o "$DATA_DIR/jakarta.osm.pbf"
  echo "  → jakarta.osm.pbf created ($(du -sh "$DATA_DIR/jakarta.osm.pbf" | cut -f1))"
else
  echo "  → jakarta.osm.pbf already exists, skipping"
fi

echo "⚙️  Step 4/5: Building OSRM routing index (this takes ~5 min)..."
PROFILE="/data/profiles/motorcycle.lua"

docker run --rm \
  -v "$DATA_DIR:/data" \
  -v "$PROFILE_DIR:/data/profiles" \
  ghcr.io/project-osrm/osrm-backend:v5.27.1 \
  osrm-extract -p "$PROFILE" /data/jakarta.osm.pbf

docker run --rm \
  -v "$DATA_DIR:/data" \
  ghcr.io/project-osrm/osrm-backend:v5.27.1 \
  osrm-partition /data/jakarta.osrm

docker run --rm \
  -v "$DATA_DIR:/data" \
  ghcr.io/project-osrm/osrm-backend:v5.27.1 \
  osrm-customize /data/jakarta.osrm

echo "✅ Step 5/5: Done! Start OSRM with:"
echo "   docker compose -f docker-compose.dev.yml --profile osrm up -d osrm"
echo ""
echo "Test routing:"
echo "   curl 'http://localhost:5000/route/v1/driving/106.8272,-6.1754;106.8200,-6.2000?overview=false'"
