# How to Add a Bandcamp Watchlist in NetRunner

NetRunner supports adding Bandcamp artist RSS feeds as watchlists to automatically acquire new track releases.

## Finding Your Bandcamp RSS Feed

Most Bandcamp artists provide an RSS feed at:
```
https://artistname.bandcamp.com/feed
```

Replace `artistname` with the actual Bandcamp artist/subdomain name. For example:
- https://exampleartist.bandcamp.com/feed
- https://labelname.bandcamp.com/feed

## Adding the Watchlist

### Via Web UI
1. Navigate to **Watchlists** in the NetRunner UI
2. Click **"Add Watchlist"**
3. Set:
   - **Name**: Descriptive name for the watchlist (e.g., "Example Artist New Releases")
   - **Type**: `rss_feed`
   - **Source URI**: The Bandcamp feed URL (e.g., `https://exampleartist.bandcamp.com/feed`)
   - **Quality Profile**: Select your preferred quality profile

### Via CLI
```bash
netrunner-cli watchlist add "Example Artist New Releases" rss_feed "https://exampleartist.bandcamp.com/feed"
```

### Via API
POST to `/api/watchlists`:
```json
{
  "name": "Example Artist New Releases",
  "source_type": "rss_feed",
  "source_uri": "https://exampleartist.bandcamp.com/feed",
  "quality_profile_id": 1
}
```

## What Metadata Is Available

When processing a Bandcamp RSS feed, NetRunner extracts:

| Field | Source | Notes |
|-------|--------|-------|
| Artist | Feed's `<channel><title>` | Used when track titles don't contain " - " separator |
| Title | Item's `<title>` | Track name only (no artist prefix) |
| Cover Art URL | Item's `<media:content>` or `<image>` | Standard RSS media extensions |
| Source Link | Item's `<link>` | Usually links to the Bandcamp track page |
| Audio File URL | Item's `<enclosure>` | Direct link to MP3/Audio file (used internally for acquisition) |

## Format Support

NetRunner's RSS provider handles both formats commonly found in Bandcamp feeds:

1. **Track-only titles** (most common in Bandcamp):
   ```xml
   <title>Track Name</title>
   ```
   → Uses channel title as artist name

2. **Artist - Title format** (sometimes used):
   ```xml
   <title>Artist Name - Track Name</title>
   ```
   → Splits on " - " to extract artist and title

## Example Feed Structure

A typical Bandcamp artist RSS feed looks like:
```xml
<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/">
  <channel>
    <title>Artist Name</title>
    <link>https://artistname.bandcamp.com</link>
    <description>New releases from Artist Name</description>
    <item>
      <title>New Track Title</title>
      <link>https://artistname.bandcamp.com/track/new-track-title</link>
      <pubDate>Wed, 11 Mar 2026 10:00:00 +0000</pubDate>
      <enclosure url="https://artistname.bandcamp.com/track/new-track-title/download" 
                 length="1234567" 
                 type="audio/mpeg" />
      <media:content url="https://f4.bcbits.com/img/a1234567890_10.jpg" />
    </item>
    <!-- More items... -->
  </channel>
</rss>
```

## Notes

- Bandcamp enclosure URLs point directly to audio files, which NetRunner uses for acquisition via slskd
- The provider prioritizes `<media:content>` for cover art URLs (standard in many RSS feeds)
- If a feed returns HTTP 404 or is unreachable, NetRunner will return an error that appears in the UI
- For best results, ensure the Bandcamp artist's feed is public and regularly updated