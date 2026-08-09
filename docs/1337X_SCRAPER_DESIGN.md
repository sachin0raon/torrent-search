# 1337x Scraper Integration — Design & Specification

## 1. Understanding Summary

* **What is being built:** A new 1337x scraper service in the FastAPI backend (`backend/app/services/x1337.py`) and a dedicated **1337x** tab in the React frontend UI alongside Torrentio and Forum.
* **Why it exists:** To aggregate public 1337x torrent search results via the static D1 API (`https://1337x-d1-static-api.zindex.eu.org/d1-web-api`), allowing raw string searches without requiring TMDB ID mapping.
* **Who it is for:** Local single-user setup of Torrent Search Aggregator seeking broader torrent options and raw text searches.
* **Key constraints:**
  * Uses direct text-query search (`/search/{query}/1/`).
  * Lazy-fetches magnet links on demand when a torrent row's Stream or Copy Magnet action is triggered.
  * Standardizes item attributes (`title`, `seeds`, `leeches`, `size`, `detail_path`).
* **Explicit non-goals:**
  * Private tracker authentication / user logins.
  * Cloudflare bypass mechanisms (direct HTTP requests to `1337x-d1-static-api.zindex.eu.org` succeed with HTTP 200).

---

## 2. Assumptions

1. **Static API Access:** Standard server-side `httpx` in Python works reliably directly against the static D1 API (`https://1337x-d1-static-api.zindex.eu.org/d1-web-api/{hash}`), so no client-side Cloudflare bypass or browser toggle is required.
2. **Search & Magnet Flow:**
   * Step 1: User types any search query -> `GET /api/1337x/search?q=...` returns the list of torrents (title, size, seeds, leeches, detail URL path).
   * Step 2: Clicking "Stream" or "Copy Magnet" lazy-fetches the magnet from `GET /api/1337x/magnet?path=...`.
3. **UI Integration:** A dedicated **1337x** tab rendered in the UI with search results list, loading indicators, and stream/copy buttons.

---

## 3. Decision Log

| Decision | Alternatives Considered | Rationale |
|----------|-------------------------|-----------|
| **Dedicated UI Tab** | Combined results with Torrentio/Forum | Keeps data source boundaries clean and lets users view 1337x-specific seed/leech counts without cluttering existing tabs. |
| **Lazy Magnet Resolution** | Pre-fetching all 20+ detail pages during initial search | Lazy fetching yields ~300ms search responses and prevents unnecessary network requests for torrents the user never opens. |
| **Direct D1 Static API** | Web scraping `1337x.to` directly or using selenium | The D1 static API provides fast HTML mirror responses without Cloudflare IP blocking issues. |
| **Raw String Search** | Strict TMDB title/year matching | Allows flexible keyword searching for titles that lack TMDB metadata or exact matches. |

---

## 4. Final Design Specification

### Backend Architecture

* **`backend/app/services/x1337.py`**:
  * `hash_path(path: str) -> str`: SHA256 of `https://1337x.to` + path.
  * `search_1337x(query: str) -> list[X1337SearchItem]`: Queries D1 API, parses HTML table rows (`tr`), extracts title, seeds, leeches, size, detail path.
  * `fetch_magnet(torrent_path: str) -> str`: Queries detail page HTML from D1 API, extracts `magnet:?xt=...` link.
* **`backend/app/routers/x1337.py`**:
  * `GET /api/1337x/search?q={query}`
  * `GET /api/1337x/magnet?path={path}`
* **`backend/app/models.py`**:
  * `X1337SearchItem` & `X1337MagnetResponse`

### Frontend Architecture

* Adds a **1337x** tab to the main search view.
* Renders search results list with title, size, seeds (green), leeches (red).
* Triggers lazy magnet fetch when Stream or Copy Magnet is clicked, caching magnet in local component state.

### Testing Strategy

* **Backend Unit Tests (`backend/tests/test_1337x.py`)**: Mock HTML fixtures for search and detail pages; test path hashing, HTML parsing, error handling.
* **Frontend Tests (`frontend/src/tests/x1337.test.tsx`)**: Test tab rendering, lazy magnet fetch interaction, and retry flow on network failure.
