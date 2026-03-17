# Phase 3 Design: Statistics/Dashboard

## Overview
Enhance the dashboard with richer analytics and statistics for better operational visibility.

## Goals
1. **Job Analytics** - Success/failure rates, trends over time, per-job-type breakdown
2. **Library Analytics** - Track counts, storage size, format distribution
3. **Activity Metrics** - Downloads per day/week, artist discovery trends
4. **API Endpoints** - Expose stats for programmatic access

## Current State
- Basic job counts (24h) already exists in `dashboard.go`
- Shows: queued, running, succeeded, failed counts
- Shows: recent jobs, watchlists, quality profiles

## Proposed Enhancements

### 1. Job Analytics (Extended)
- **Success rate**: Calculate % of succeeded vs failed
- **By job type**: Breakdown per type (sync, scan, acquisition, artist_scan)
- **Trends**: Jobs per day for last 7/30 days
- **Average duration**: Track job completion times
- **Failure reasons**: Aggregate error summaries from job.error_detail

### 2. Library Analytics
- **Total tracks**: Count from tracks table
- **Storage used**: Sum of file_size from tracks
- **Format distribution**: Count by format (FLAC, MP3, etc.)
- **By library**: Per-library breakdown

### 3. Activity Metrics  
- **Downloads**: Total acquisitions over time
- **Artists monitored**: Count of monitored_artists
- **New artists**: Track new artist additions over time
- **Watchlist activity**: Last sync times

### 4. API Endpoints
- `GET /api/stats/jobs` - Job statistics
- `GET /api/stats/library` - Library statistics
- `GET /api/stats/activity` - Activity metrics
- `GET /api/stats/summary` - Combined overview

## Database Queries

### Job Stats
```sql
-- Last 7 days by day
SELECT DATE(requested_at) as date, state, COUNT(*) as count 
FROM jobs 
WHERE requested_at > NOW() - INTERVAL '7 days'
GROUP BY DATE(requested_at), state;

-- By job type
SELECT job_type, state, COUNT(*) as count 
FROM jobs 
WHERE requested_at > NOW() - INTERVAL '30 days'
GROUP BY job_type, state;
```

### Library Stats
```sql
-- Format distribution
SELECT format, COUNT(*) as count, SUM(file_size) as total_size 
FROM tracks 
GROUP BY format;

-- By library
SELECT library_id, COUNT(*) as track_count, SUM(file_size) as total_size 
FROM tracks 
GROUP BY library_id;
```

## CLI Commands (Optional)
- `netrunner-cli stats` - Show summary stats
- `netrunner-cli stats jobs` - Job analytics
- `netrunner-cli stats library` - Library analytics

## Acceptance Criteria
1. Dashboard shows enhanced job analytics with success rates and trends
2. Library analytics show track counts and storage estimates
3. API endpoints return JSON statistics
4. Tests cover new statistics endpoints
5. Existing dashboard functionality preserved
