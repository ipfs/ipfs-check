package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetCIDSuffix(t *testing.T) {
	t.Run("long CID returns last 6 characters", func(t *testing.T) {
		cid := "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
		result := getCIDSuffix(cid)
		require.Equal(t, "5fbzdi", result)
	})

	t.Run("exactly 6 characters returns full string", func(t *testing.T) {
		cid := "abcdef"
		result := getCIDSuffix(cid)
		require.Equal(t, "abcdef", result)
	})

	t.Run("less than 6 characters returns full string", func(t *testing.T) {
		cid := "abc"
		result := getCIDSuffix(cid)
		require.Equal(t, "abc", result)
	})

	t.Run("empty string returns empty", func(t *testing.T) {
		result := getCIDSuffix("")
		require.Equal(t, "", result)
	})
}

func TestGenerateBadgeSVG(t *testing.T) {
	t.Run("0 providers shows red unavailable badge", func(t *testing.T) {
		svg := generateBadgeSVG(0, "abc123")
		svgStr := string(svg)

		require.Contains(t, svgStr, "unavailable ...abc123")
		require.Contains(t, svgStr, colorRed)
		require.Contains(t, svgStr, "IPFS")
	})

	t.Run("1 provider shows yellow warning badge", func(t *testing.T) {
		svg := generateBadgeSVG(1, "abc123")
		svgStr := string(svg)

		require.Contains(t, svgStr, "1 provider ...abc123")
		require.Contains(t, svgStr, colorYellow)
		require.Contains(t, svgStr, "IPFS")
	})

	t.Run("2 providers shows green badge", func(t *testing.T) {
		svg := generateBadgeSVG(2, "abc123")
		svgStr := string(svg)

		require.Contains(t, svgStr, "2 providers ...abc123")
		require.Contains(t, svgStr, colorGreen)
		require.Contains(t, svgStr, "IPFS")
	})

	t.Run("10 providers shows green badge with plural", func(t *testing.T) {
		svg := generateBadgeSVG(10, "xyz789")
		svgStr := string(svg)

		require.Contains(t, svgStr, "10 providers ...xyz789")
		require.Contains(t, svgStr, colorGreen)
	})

	t.Run("SVG has valid structure", func(t *testing.T) {
		svg := generateBadgeSVG(5, "test12")
		svgStr := string(svg)

		require.True(t, strings.HasPrefix(svgStr, "<svg"))
		require.True(t, strings.HasSuffix(strings.TrimSpace(svgStr), "</svg>"))
		require.Contains(t, svgStr, "xmlns=\"http://www.w3.org/2000/svg\"")
	})
}

func TestGenerateErrorBadgeSVG(t *testing.T) {
	t.Run("error badge has gray color and error text", func(t *testing.T) {
		svg := generateErrorBadgeSVG("abc123")
		svgStr := string(svg)

		require.Contains(t, svgStr, "error ...abc123")
		require.Contains(t, svgStr, colorGray)
		require.Contains(t, svgStr, "IPFS")
	})
}

func TestGeneratePendingBadgeSVG(t *testing.T) {
	t.Run("pending badge has gray color and checking text", func(t *testing.T) {
		svg := generatePendingBadgeSVG("abc123")
		svgStr := string(svg)

		require.Contains(t, svgStr, "checking ...abc123")
		require.Contains(t, svgStr, colorGray)
		require.Contains(t, svgStr, "IPFS")
	})
}

func TestCountWorkingProviders(t *testing.T) {
	t.Run("nil providers returns 0", func(t *testing.T) {
		count := countWorkingProviders(nil)
		require.Equal(t, 0, count)
	})

	t.Run("empty providers returns 0", func(t *testing.T) {
		providers := []providerOutput{}
		count := countWorkingProviders(&providers)
		require.Equal(t, 0, count)
	})

	t.Run("provider with connection error not counted", func(t *testing.T) {
		providers := []providerOutput{
			{
				ConnectionError:          "connection refused",
				DataAvailableOverBitswap: BitswapCheckOutput{Found: true},
			},
		}
		count := countWorkingProviders(&providers)
		require.Equal(t, 0, count)
	})

	t.Run("provider with bitswap data counted", func(t *testing.T) {
		providers := []providerOutput{
			{
				ConnectionError:          "",
				DataAvailableOverBitswap: BitswapCheckOutput{Found: true},
			},
		}
		count := countWorkingProviders(&providers)
		require.Equal(t, 1, count)
	})

	t.Run("provider with HTTP data counted", func(t *testing.T) {
		providers := []providerOutput{
			{
				ConnectionError:       "",
				DataAvailableOverHTTP: HTTPCheckOutput{Found: true},
			},
		}
		count := countWorkingProviders(&providers)
		require.Equal(t, 1, count)
	})

	t.Run("provider with both bitswap and HTTP counted once", func(t *testing.T) {
		providers := []providerOutput{
			{
				ConnectionError:          "",
				DataAvailableOverBitswap: BitswapCheckOutput{Found: true},
				DataAvailableOverHTTP:    HTTPCheckOutput{Found: true},
			},
		}
		count := countWorkingProviders(&providers)
		require.Equal(t, 1, count)
	})

	t.Run("multiple working providers counted correctly", func(t *testing.T) {
		providers := []providerOutput{
			{
				ConnectionError:          "",
				DataAvailableOverBitswap: BitswapCheckOutput{Found: true},
			},
			{
				ConnectionError:       "",
				DataAvailableOverHTTP: HTTPCheckOutput{Found: true},
			},
			{
				ConnectionError:          "timeout",
				DataAvailableOverBitswap: BitswapCheckOutput{Found: true},
			},
		}
		count := countWorkingProviders(&providers)
		require.Equal(t, 2, count)
	})

	t.Run("connected but no data not counted", func(t *testing.T) {
		providers := []providerOutput{
			{
				ConnectionError:          "",
				DataAvailableOverBitswap: BitswapCheckOutput{Found: false},
				DataAvailableOverHTTP:    HTTPCheckOutput{Found: false},
			},
		}
		count := countWorkingProviders(&providers)
		require.Equal(t, 0, count)
	})
}

func TestBadgeCache(t *testing.T) {
	t.Run("Get returns false for non-existent key", func(t *testing.T) {
		cache := NewBadgeCache(time.Hour)
		_, ok := cache.Get("nonexistent")
		require.False(t, ok)
	})

	t.Run("Set and Get work correctly", func(t *testing.T) {
		cache := NewBadgeCache(time.Hour)
		svg := []byte("<svg>test</svg>")

		cache.Set("cid123", 5, "id123", svg)

		entry, ok := cache.Get("cid123")
		require.True(t, ok)
		require.Equal(t, 5, entry.ProviderCount)
		require.Equal(t, "id123", entry.CIDSuffix)
		require.Equal(t, svg, entry.SVG)
		require.False(t, entry.Pending)
	})

	t.Run("SetPending marks entry as pending", func(t *testing.T) {
		cache := NewBadgeCache(time.Hour)
		svg := []byte("<svg>pending</svg>")

		cache.SetPending("cid123", "id123", svg)

		entry, ok := cache.Get("cid123")
		require.True(t, ok)
		require.True(t, entry.Pending)
	})

	t.Run("IsPending returns correct state", func(t *testing.T) {
		cache := NewBadgeCache(time.Hour)

		require.False(t, cache.IsPending("nonexistent"))

		cache.SetPending("cid123", "id123", []byte{})
		require.True(t, cache.IsPending("cid123"))

		cache.Set("cid123", 5, "id123", []byte{})
		require.False(t, cache.IsPending("cid123"))
	})

	t.Run("expired entries are not returned", func(t *testing.T) {
		cache := NewBadgeCache(10 * time.Millisecond)
		cache.Set("cid123", 5, "id123", []byte{})

		entry, ok := cache.Get("cid123")
		require.True(t, ok)
		require.Equal(t, 5, entry.ProviderCount)

		time.Sleep(20 * time.Millisecond)

		_, ok = cache.Get("cid123")
		require.False(t, ok)
	})

	t.Run("Size returns correct count", func(t *testing.T) {
		cache := NewBadgeCache(time.Hour)
		require.Equal(t, 0, cache.Size())

		cache.Set("cid1", 1, "id1", []byte{})
		require.Equal(t, 1, cache.Size())

		cache.Set("cid2", 2, "id2", []byte{})
		require.Equal(t, 2, cache.Size())

		cache.Set("cid1", 3, "id1", []byte{})
		require.Equal(t, 2, cache.Size())
	})

	t.Run("cleanup removes expired entries", func(t *testing.T) {
		cache := NewBadgeCache(10 * time.Millisecond)
		cache.Set("cid1", 1, "id1", []byte{})
		cache.Set("cid2", 2, "id2", []byte{})

		require.Equal(t, 2, cache.Size())

		time.Sleep(20 * time.Millisecond)
		cache.cleanup()

		require.Equal(t, 0, cache.Size())
	})
}

func TestBadgeHandlerServeHTTP(t *testing.T) {
	t.Run("missing cid parameter returns 400", func(t *testing.T) {
		cache := NewBadgeCache(time.Hour)
		handler := &BadgeHandler{
			daemon:       nil,
			cache:        cache,
			checkTimeout: 30 * time.Second,
		}

		req := httptest.NewRequest(http.MethodGet, "/badge", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "missing 'cid' query parameter")
	})

	t.Run("cached result is returned", func(t *testing.T) {
		cache := NewBadgeCache(time.Hour)
		svg := generateBadgeSVG(3, "abc123")
		cache.Set("testcid123", 3, "abc123", svg)

		handler := &BadgeHandler{
			daemon:       nil,
			cache:        cache,
			checkTimeout: 30 * time.Second,
		}

		req := httptest.NewRequest(http.MethodGet, "/badge?cid=testcid123", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "image/svg+xml", w.Header().Get("Content-Type"))
		require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
		require.Contains(t, w.Header().Get("Cache-Control"), "max-age=3600")
		require.Contains(t, w.Body.String(), "3 providers ...abc123")
	})

	t.Run("pending result returns pending badge", func(t *testing.T) {
		cache := NewBadgeCache(time.Hour)
		pendingSVG := generatePendingBadgeSVG("xyz789")
		cache.SetPending("pendingcid", "xyz789", pendingSVG)

		handler := &BadgeHandler{
			daemon:       nil,
			cache:        cache,
			checkTimeout: 30 * time.Second,
		}

		req := httptest.NewRequest(http.MethodGet, "/badge?cid=pendingcid", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), "checking ...xyz789")
	})

	t.Run("CORS header is set", func(t *testing.T) {
		cache := NewBadgeCache(time.Hour)
		cache.Set("testcid", 1, "testci", []byte("<svg></svg>"))

		handler := &BadgeHandler{
			daemon:       nil,
			cache:        cache,
			checkTimeout: 30 * time.Second,
		}

		req := httptest.NewRequest(http.MethodGet, "/badge?cid=testcid", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	})
}

func TestBuildBadgeSVG(t *testing.T) {
	t.Run("builds valid SVG with correct dimensions", func(t *testing.T) {
		svg := buildBadgeSVG("TEST", "status text", "#ff0000")
		svgStr := string(svg)

		require.Contains(t, svgStr, "xmlns=\"http://www.w3.org/2000/svg\"")
		require.Contains(t, svgStr, "TEST")
		require.Contains(t, svgStr, "status text")
		require.Contains(t, svgStr, "#ff0000")
		require.Contains(t, svgStr, "height=\"20\"")
	})

	t.Run("label and status are both rendered twice for shadow effect", func(t *testing.T) {
		svg := buildBadgeSVG("LABEL", "STATUS", "#00ff00")
		svgStr := string(svg)

		require.Equal(t, 2, strings.Count(svgStr, ">LABEL<"))
		require.Equal(t, 2, strings.Count(svgStr, ">STATUS<"))
	})
}
