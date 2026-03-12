# Initial Concept
Djinn NETRUNNER: Console-first, self-hosted music acquisition and streaming appliance.

# Product Guide: Djinn NETRUNNER

## Overview
Djinn NETRUNNER is a console-first, self-hosted music acquisition and streaming appliance. It provides a complete system for automated music discovery, download, organization, and streaming with Subsonic compatibility.

## Core Purpose
The project aims to provide a robust, self-hosted music pipeline that automates the acquisition of music from Soulseek, organizes files with smart metadata extraction, and provides a terminal-aesthetic operations console for monitoring and management.

## Key Features
- **Universal Watchlists:** Sync playlists and collections from Spotify, Last.fm, ListenBrainz, RSS feeds, Discogs, and Local Files (M3U, CSV, TXT) with automated polling and discovery.
- **Intelligent Search:** Quality-based ranking, custom quality profiles, and concurrent download management via slskd.
- **Library Integrity:** Smart indexing skip via Gonic API and automated cover art embedding (ID3/FLAC).
- **Multi-node Coordination:** Production-grade data layer supporting distributed workers via LiteFS.
- **Operations Console:** Real-time log streaming with WebSockets and terminal-inspired UI.
- **Job Orchestration:** Crash-safe job scheduling with deterministic work plans and state machines.
- **Privacy Proxy:** Integrated proxy support for secure acquisition.

## Target Audience
Music enthusiasts and self-hosters who want a reliable, automated, and visually unique (terminal-aesthetic) music library management system.
