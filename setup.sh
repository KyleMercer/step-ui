#!/bin/bash

# Step-CA Web UI Setup Script

set -e

cd "$(dirname "$0")"

# Just informational here (this script always builds via plain
# `docker compose up -d --build`, unversioned) -- lets you see, before
# anything runs, which VERSION you're about to bring up. For a build that
# actually bakes this into the images as OCI labels, use ./build.sh instead.
APP_VERSION="$(cat VERSION 2>/dev/null || echo unknown)"

echo "🚀 Setting up Step-CA Web UI (VERSION ${APP_VERSION})..."
echo "📦 Repository: https://github.com/marcin-kruszynski/step-ui.git"

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed. Please install Docker first."
    exit 1
fi

# Check if docker compose is available
if ! docker compose version &> /dev/null; then
    echo "❌ Docker Compose is not available. Please ensure Docker Desktop is running and WSL integration is enabled."
    exit 1
fi

# Create necessary directories
echo "📁 Creating directories..."
mkdir -p data logs

# Copy environment file if it doesn't exist
if [ ! -f .env ]; then
    echo "📝 Creating .env file..."
    cp example.env .env
    echo "✅ Created .env file. Please edit it with your Step-CA configuration."
else
    echo "✅ .env file already exists."
fi

# Load environment variables from .env file
if [ -f .env ]; then
    echo "📄 Loading environment variables from .env file..."
    export $(grep -v '^#' .env | xargs)
fi

# Check environment configuration
echo "🔐 Checking configuration..."
if [ -z "$CA_URL" ]; then
    echo "⚠️  CA_URL not set in .env file. Please configure your Step-CA URL."
fi

if [ -z "$PROVISIONER_PASSWORD" ]; then
    echo "⚠️  PROVISIONER_PASSWORD not set in .env file. Please configure your provisioner password."
fi

# Build and start services
echo "🔨 Building and starting services..."
docker compose up -d --build

echo ""
echo "🎉 Setup complete!"
echo ""
echo "📋 Next steps:"
echo "1. Edit .env file with your Step-CA configuration"
echo "2. Restart services: docker compose restart"
echo ""
echo "🌐 Access the application:"
echo "   Frontend: http://localhost:3000"
echo "   Backend API: http://localhost:8080"
echo ""
echo "📖 For more information, see README.md"
